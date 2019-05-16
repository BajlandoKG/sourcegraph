import SettingsIcon from 'mdi-react/SettingsIcon'
import SourcePullIcon from 'mdi-react/SourcePullIcon'
import ViewListIcon from 'mdi-react/ViewListIcon'
import React from 'react'
import { NavLink } from 'react-router-dom'
import { ChatIcon } from '../../../../../../shared/src/components/icons'
import { ExtensionsControllerProps } from '../../../../../../shared/src/extensions/controller'
import * as GQL from '../../../../../../shared/src/graphql/schema'
import { ThreadSettings } from '../../settings'

interface Props extends ExtensionsControllerProps {
    thread: GQL.IDiscussionThread
    onThreadUpdate: (thread: GQL.IDiscussionThread) => void
    threadSettings: ThreadSettings
    areaURL: string
    className?: string
}

/**
 * The sidebar for the thread area (for a single thread).
 */
export const ThreadAreaSidebar: React.FunctionComponent<Props> = ({
    thread,
    onThreadUpdate,
    threadSettings,
    areaURL,
    className = '',
}) => (
    <aside className={`thread-area-sidebar d-flex flex-column ${className}`}>
        <ul className="nav flex-column">
            <li className="nav-item">
                <NavLink
                    to={areaURL}
                    className="thread-area-sidebar__nav-link nav-link"
                    activeClassName="active thread-area-sidebar__nav-link--active"
                    exact={true}
                >
                    <ChatIcon className="icon-inline" /> Conversation{' '}
                    <span className="badge badge-secondary">{thread.comments.totalCount}</span>
                </NavLink>
            </li>
            <li className="nav-item">
                <NavLink
                    to={`${areaURL}/sources`}
                    className="thread-area-sidebar__nav-link nav-link"
                    activeClassName="active thread-area-sidebar__nav-link--active"
                    exact={true}
                >
                    <ViewListIcon className="icon-inline" /> Sources{' '}
                    <span className="badge badge-secondary">{thread.targets.totalCount}</span>
                </NavLink>
            </li>
            <li className="nav-item">
                <NavLink
                    to={`${areaURL}/activity`}
                    className="thread-area-sidebar__nav-link nav-link"
                    activeClassName="active thread-area-sidebar__nav-link--active"
                    exact={true}
                >
                    <SourcePullIcon className="icon-inline" /> Changes{' '}
                    {threadSettings.createPullRequests && <span className="badge badge-secondary">50%</span>}
                </NavLink>
            </li>
            <li className="nav-item">
                <NavLink
                    to={`${areaURL}/manage`}
                    className="thread-area-sidebar__nav-link nav-link"
                    activeClassName="active thread-area-sidebar__nav-link--active"
                    exact={true}
                >
                    <SettingsIcon className="icon-inline" /> Manage
                </NavLink>
            </li>
        </ul>
    </aside>
)
