import * as React from 'react'
import { RouteComponentProps } from 'react-router'
import { concat, Subject, Subscription } from 'rxjs'
import { catchError, map, switchMap } from 'rxjs/operators'
import { Omit } from 'utility-types'
import * as GQL from '../../../../shared/src/graphql/schema'
import { ErrorLike, isErrorLike } from '../../../../shared/src/util/errors'
import { createSavedSearch } from '../../search/backend'
import { SavedQueryFields, SavedSearchForm } from '../../search/saved-searches/SavedSearchForm'

interface Props extends RouteComponentProps {
    /** The URL path to return to after successfully creating a saved search.  */
    returnPath: string
    authenticatedUser: GQL.IUser | null
    emailNotificationLabel: string
    orgID?: GQL.ID
    userID?: GQL.ID
}

const LOADING: 'loading' = 'loading'

interface State {
    createdOrError: undefined | typeof LOADING | true | ErrorLike
}

export class SavedSearchCreateForm extends React.Component<Props, State> {
    constructor(props: Props) {
        super(props)
        this.state = {
            createdOrError: undefined,
        }
    }
    private subscriptions = new Subscription()
    private submits = new Subject<Omit<SavedQueryFields, 'id'>>()

    public componentDidMount(): void {
        this.subscriptions.add(
            this.submits
                .pipe(
                    switchMap(fields =>
                        concat(
                            [LOADING],
                            createSavedSearch(
                                fields.description,
                                fields.query,
                                fields.notify,
                                fields.notifySlack,
                                fields.userID,
                                fields.orgID
                            ).pipe(
                                map(() => true),
                                catchError(error => [error])
                            )
                        )
                    )
                )
                .subscribe(createdOrError => {
                    this.setState({ createdOrError })
                    if (createdOrError === true) {
                        this.props.history.push(this.props.returnPath)
                    }
                })
        )
    }

    public render(): JSX.Element | null {
        const q = new URLSearchParams(this.props.location.search)
        let defaultValue: Partial<SavedQueryFields> = {}
        const query = q.get('query')
        if (query) {
            defaultValue = { query }
        }

        return (
            <>
                <SavedSearchForm
                    {...this.props}
                    submitLabel="Add saved search"
                    title="Add saved search"
                    defaultValues={
                        this.props.orgID
                            ? { orgID: this.props.orgID, ...defaultValue }
                            : { userID: this.props.userID, ...defaultValue }
                    }
                    onSubmit={this.onSubmit}
                    loading={this.state.createdOrError === LOADING}
                    error={isErrorLike(this.state.createdOrError) ? this.state.createdOrError : undefined}
                />
            </>
        )
    }

    private onSubmit = (fields: Omit<SavedQueryFields, 'id'>) => this.submits.next(fields)
}
