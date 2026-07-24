// Semantic colour tones shared across webviews, backed by the --gx-* tokens.
// Components take a tone; callers map their domain (a run state, a verb kind, a
// deploy outcome) onto one. Keeps colour meaning in one vocabulary.
export type Tone =
	| "deploy"
	| "enabled"
	| "disabled"
	| "deleted"
	| "warn"
	| "quiet"
	| "rewrite";
