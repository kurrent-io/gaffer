import outcomes from "./outcomes.module.css";
import styles from "./OutcomeLabel.module.css";

// A projection's outcome, uppercase and coloured by the outcome. `text` is the
// word to show (the plan's would-do verb, or a live result word).
export function OutcomeLabel(props: { outcome: string; text: string }) {
	return (
		<span
			class={[styles.outcome, outcomes[props.outcome]]
				.filter(Boolean)
				.join(" ")}
		>
			{props.text}
		</span>
	);
}
