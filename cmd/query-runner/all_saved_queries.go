package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/sourcegraph/cmd/query-runner/queryrunnerapi"
	"github.com/sourcegraph/sourcegraph/pkg/api"
	log15 "gopkg.in/inconshreveable/log15.v2"
)

var allSavedQueries = &allSavedQueriesCached{}

// allSavedQueriesCached allows us to get a list of all the saved queries
// configured for every user/org on the entire server, without the overhead of
// constantly querying, unmarshaling, and transferring over the network all of
// the saved query setting values. Instead, we ask for the list once on startup
// and frontend instances notify us of created/updated/deleted saved queries in
// user/org configurations.
type allSavedQueriesCached struct {
	mu              sync.Mutex
	allSavedQueries map[string]api.SavedQuerySpecAndConfig
}

func savedQueryIDSpecKey(s api.SavedQueryIDSpec) string {
	return s.Subject.String() + s.Key
}

// get returns a copy of sq.allSavedQueries to avoid retaining the lock and
// blocking other oparations that call savedQueryWas[Created|Updated|Deleted]
// which also need the lock.
func (sq *allSavedQueriesCached) get() map[string]api.SavedQuerySpecAndConfig {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	cpy := make(map[string]api.SavedQuerySpecAndConfig, len(sq.allSavedQueries))
	for k, v := range sq.allSavedQueries {
		cpy[k] = v
	}
	return cpy
}

// fetchInitialListFromFrontend blocks until the initial list can be initialized.
func (sq *allSavedQueriesCached) fetchInitialListFromFrontend() {
	sq.mu.Lock()
	defer sq.mu.Unlock()

	attempts := 0
	for {
		allSavedQueries, err := api.InternalClient.SavedQueriesListAll(context.Background())
		if err != nil {
			if attempts > 3 {
				// Only print the error if we've retried a few times, otherwise
				// we would be needlessly verbose when the frontend just hasn't
				// started yet but will soon.
				log15.Error("executor: error fetching saved queries list (trying again in 5s)", "error", err)
			}
			time.Sleep(5 * time.Second)
			attempts++
			continue
		}
		sq.allSavedQueries = make(map[string]api.SavedQuerySpecAndConfig, len(allSavedQueries))
		for spec, config := range allSavedQueries {
			sq.allSavedQueries[savedQueryIDSpecKey(spec)] = api.SavedQuerySpecAndConfig{
				Spec:   spec,
				Config: config.Config,
			}
		}
		log15.Debug("existing saved queries detected", "total_saved_queries", len(sq.allSavedQueries))
		return
	}
}

func serveSavedQueryWasCreatedOrUpdated(w http.ResponseWriter, r *http.Request) {
	allSavedQueries.mu.Lock()
	defer allSavedQueries.mu.Unlock()

	var args *queryrunnerapi.SavedQueryWasCreatedOrUpdatedArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeError(w, errors.Wrap(err, "decoding JSON arguments"))
		return
	}

	for _, query := range args.SubjectAndConfig.Config.SavedQueries {
		spec := api.SavedQueryIDSpec{Subject: args.SubjectAndConfig.Subject, Key: query.Key}
		key := savedQueryIDSpecKey(spec)
		newValue := api.SavedQuerySpecAndConfig{
			Spec:   spec,
			Config: query,
		}

		oldValue := allSavedQueries.allSavedQueries[key]
		if !args.DisableSubscriptionNotifications {
			// Notify users of saved query creation and updates.
			go func() {
				if err := notifySavedQueryWasCreatedOrUpdated(oldValue, newValue); err != nil {
					log15.Error("Failed to handle created/updated saved search.", "query", query, "error", err)
				}
			}()
		}

		allSavedQueries.allSavedQueries[key] = newValue
	}
	log15.Info("saved query created or updated", "total_saved_queries", len(allSavedQueries.allSavedQueries))
	w.WriteHeader(http.StatusOK)
}

func serveSavedQueryWasDeleted(w http.ResponseWriter, r *http.Request) {
	allSavedQueries.mu.Lock()
	defer allSavedQueries.mu.Unlock()

	var args *queryrunnerapi.SavedQueryWasDeletedArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeError(w, errors.Wrap(err, "decoding JSON arguments"))
		return
	}

	key := savedQueryIDSpecKey(args.Spec)
	query, ok := allSavedQueries.allSavedQueries[key]
	if !ok {
		return // query to delete already doesn't exist; do nothing
	}
	delete(allSavedQueries.allSavedQueries, key)

	if !args.DisableSubscriptionNotifications {
		// Notify users of saved query deletions.
		go func() {
			if err := notifySavedQueryWasCreatedOrUpdated(query, api.SavedQuerySpecAndConfig{}); err != nil {
				log15.Error("Failed to handle created/updated saved search.", "query", query, "error", err)
			}
		}()
	}

	// Delete from database, but only if another saved query is not the same.
	anotherExists := false
	for _, other := range allSavedQueries.allSavedQueries {
		if other.Config.Query == query.Config.Query {
			anotherExists = true
			break
		}
	}
	if !anotherExists {
		if err := api.InternalClient.SavedQueriesDeleteInfo(r.Context(), query.Config.Query); err != nil {
			log15.Error("Failed to delete saved query from DB: SavedQueriesDeleteInfo", "error", err)
			return
		}
	}
	log15.Info("saved query deleted", "total_saved_queries", len(allSavedQueries.allSavedQueries))
}

// diffSavedQueryConfigs takes the old and new saved queries configurations.
//
// It returns maps whose keys represent the old value and value represent the
// new value, i.e. a map of the saved query in the oldList and what its new
// value is in the newList for each respective category. For deleted, the new
// value will be an empty struct.
func diffSavedQueryConfigs(oldList, newList map[api.SavedQueryIDSpec]api.SavedQuerySpecAndConfig) (deleted, updated, created map[api.SavedQuerySpecAndConfig]api.SavedQuerySpecAndConfig) {
	deleted = map[api.SavedQuerySpecAndConfig]api.SavedQuerySpecAndConfig{}
	updated = map[api.SavedQuerySpecAndConfig]api.SavedQuerySpecAndConfig{}
	created = map[api.SavedQuerySpecAndConfig]api.SavedQuerySpecAndConfig{}

	// Because the key api.SavedQueryIDSpec contains pointers, we should use
	// its unique string key.
	//
	// TODO(slimsag/farhan): long term: plz coding overlords let's make these
	// api.SavedQuery Spec types more sane / remove them (in reality, this will
	// be easy to do once we move query runner to frontend later.)
	oldByKey := make(map[string]api.SavedQuerySpecAndConfig, len(oldList))
	for k, v := range oldList {
		oldByKey[k.Key] = v
	}
	newByKey := make(map[string]api.SavedQuerySpecAndConfig, len(newList))
	for k, v := range newList {
		newByKey[k.Key] = v
	}

	// Detect deleted entries.
	for k, oldVal := range oldByKey {
		if _, ok := newByKey[k]; !ok {
			deleted[oldVal] = api.SavedQuerySpecAndConfig{}
		}
	}
	for k, newVal := range newByKey {
		// Detect created entries.
		if oldVal, ok := oldByKey[k]; !ok {
			created[oldVal] = newVal
			continue
		}

		// Detect updated entries.
		oldVal := oldByKey[k]
		if ok := reflect.DeepEqual(newVal, oldVal); !ok {
			updated[oldVal] = newVal
		}
	}
	return deleted, updated, created
}

func sendNotificationsForCreatedOrUpdatedOrDeleted(oldList, newList map[api.SavedQueryIDSpec]api.SavedQuerySpecAndConfig) {
	fmt.Println("SEND NOTIF FOR UPDATED CREATED OR DELETED")
	deleted, updated, created := diffSavedQueryConfigs(oldList, newList)
	// fmt.Println(deleted)
	// fmt.Println(updated)
	// fmt.Println(created)
	for oldVal, newVal := range deleted {
		oldVal := oldVal
		newVal := newVal
		go func() {
			if err := notifySavedQueryWasCreatedOrUpdated(oldVal, newVal); err != nil {
				log15.Error("Failed to handle deleted saved search.", "query", oldVal.Config.Query, "error", err)

			}
		}()
	}
	for oldVal, newVal := range created {
		oldVal := oldVal
		newVal := newVal
		go func() {
			if err := notifySavedQueryWasCreatedOrUpdated(oldVal, newVal); err != nil {
				log15.Error("Failed to handle deleted saved search.", "query", oldVal.Config.Query, "error", err)

			}
		}()
	}
	for oldVal, newVal := range updated {
		oldVal := oldVal
		newVal := newVal
		go func() {
			if err := notifySavedQueryWasCreatedOrUpdated(oldVal, newVal); err != nil {
				log15.Error("Failed to handle deleted saved search.", "query", oldVal.Config.Query, "error", err)

			}
		}()
	}
}

func notifySavedQueryWasCreatedOrUpdated(oldValue, newValue api.SavedQuerySpecAndConfig) error {
	ctx := context.Background()

	oldRecipients, err := getNotificationRecipients(ctx, oldValue.Spec, oldValue.Config)
	if err != nil {
		return err
	}
	newRecipients, err := getNotificationRecipients(ctx, newValue.Spec, newValue.Config)
	if err != nil {
		return err
	}

	removedRecipients, addedRecipients := diffNotificationRecipients(oldRecipients, newRecipients)
	log15.Debug("Notifying for created/updated saved search", "removed", removedRecipients, "added", addedRecipients)
	for _, removedRecipient := range removedRecipients {
		if removedRecipient.email {
			if err := emailNotifySubscribeUnsubscribe(ctx, removedRecipient, oldValue, notifyUnsubscribedTemplate); err != nil {
				log15.Error("Failed to send unsubscribed email notification.", "recipient", removedRecipient, "error", err)
			}
		}
		if removedRecipient.slack {
			if err := slackNotifyUnsubscribed(ctx, removedRecipient, oldValue); err != nil {
				log15.Error("Failed to send unsubscribed Slack notification.", "recipient", removedRecipient, "error", err)
			}
		}
	}
	for _, addedRecipient := range addedRecipients {
		if addedRecipient.email {
			if err := emailNotifySubscribeUnsubscribe(ctx, addedRecipient, newValue, notifySubscribedTemplate); err != nil {
				log15.Error("Failed to send subscribed email notification.", "recipient", addedRecipient, "error", err)
			}
		}
		if addedRecipient.slack {
			if err := slackNotifySubscribed(ctx, addedRecipient, newValue); err != nil {
				log15.Error("Failed to send subscribed Slack notification.", "recipient", addedRecipient, "error", err)
			}
		}
	}
	return nil
}

func serveTestNotification(w http.ResponseWriter, r *http.Request) {
	allSavedQueries.mu.Lock()
	defer allSavedQueries.mu.Unlock()

	var args *queryrunnerapi.TestNotificationArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		writeError(w, errors.Wrap(err, "decoding JSON arguments"))
		return
	}

	key := savedQueryIDSpecKey(args.Spec)
	query, ok := allSavedQueries.allSavedQueries[key]
	if !ok {
		writeError(w, fmt.Errorf("no saved search found with key %q", key))
		return
	}

	recipients, err := getNotificationRecipients(r.Context(), query.Spec, query.Config)
	if err != nil {
		writeError(w, fmt.Errorf("error computing recipients: %s", err))
		return
	}

	for _, recipient := range recipients {
		if err := emailNotifySubscribeUnsubscribe(r.Context(), recipient, query, notifySubscribedTemplate); err != nil {
			writeError(w, fmt.Errorf("error sending email notifications to %s: %s", recipient.spec, err))
			return
		}
		if err := slackNotify(context.Background(), recipient,
			fmt.Sprintf(`It worked! This is a test notification for the Sourcegraph saved search <%s|"%s">.`, searchURL(query.Config.Query, utmSourceSlack), query.Config.Description)); err != nil {
			writeError(w, fmt.Errorf("error sending email notifications to %s: %s", recipient.spec, err))
			return
		}
	}

	log15.Info("saved query test notification sent", "spec", args.Spec, "key", key)
}
