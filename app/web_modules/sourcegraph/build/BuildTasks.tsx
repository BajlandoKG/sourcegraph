// tslint:disable

import * as React from "react";

import Component from "sourcegraph/Component";
import TopLevelTask from "sourcegraph/build/TopLevelTask";

import CSSModules from "react-css-modules";
import * as styles from "./styles/Build.css";

class BuildTasks extends Component<any, any> {
	constructor(props) {
		super(props);
		this.state = {
			topLevelTasks: [],
			subtasks: {},
		};
	}

	reconcileState(state, props) {
		state.logs = props.logs;

		if (state.tasks !== props.tasks) {
			state.tasks = props.tasks;
			state.topLevelTasks = state.tasks.filter((task) => !task.ParentID);

			// Generate subtasks map.
			state.subtasks = {};
			state.tasks.forEach((task) => {
				state.subtasks[task.ID] = state.tasks.filter((subtask) => subtask.ParentID === task.ID);
			});
		}
	}

	render(): JSX.Element | null {
		return (
			<div className={styles.tasks}>
				{this.state.topLevelTasks.map((task, i) =>
					<TopLevelTask key={task.ID}
						task={task} subtasks={this.state.subtasks[task.ID]} logs={this.state.logs} />)}
			</div>
		);
	}
}

(BuildTasks as any).propTypes = {
	tasks: React.PropTypes.array.isRequired,
	logs: React.PropTypes.object.isRequired,
};

export default CSSModules(BuildTasks, styles);
