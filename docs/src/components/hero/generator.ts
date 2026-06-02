// Procedural generator that yields lines shaped like `gaffer dev` text
// output. Pure, DOM-agnostic. Callers iterate one Line at a time and use
// `pauseAfterMs` to decide when to ask for the next.

export type TokenKind =
	| 'plain'
	| 'pipe'
	| 'label'
	| 'heading'
	| 'processed'
	| 'skipped'
	| 'errStatus'
	| 'errDetail'
	| 'info';

export type Token = { kind: TokenKind; text: string };

export type Line = { tokens: Token[]; pauseAfterMs: number };

const PROJECTION_NAMES = [
	'order-count',
	'orders-by-customer',
	'inventory-totals',
	'customer-lifetime-value',
	'cart-abandonment',
	'daily-sales',
	'user-sessions',
	'failed-payments',
	'subscription-churn',
	'event-funnel',
];

const EVENT_TYPES = [
	'OrderPlaced',
	'OrderShipped',
	'PaymentReceived',
	'RefundIssued',
	'CartUpdated',
	'UserSignedUp',
	'UserDeactivated',
	'InventoryAdjusted',
	'SubscriptionRenewed',
	'SubscriptionCancelled',
	'EmailSent',
	'LoginFailed',
];

const CATEGORIES = ['order', 'cart', 'customer', 'payment', 'user', 'inventory', 'invoice', 'session'];

const DB_VERSIONS = ['v26.1.0', 'v26.0.5', 'v25.0.4', 'v26.1.1'];

const SKIP_REASONS = [
	'no handler for this event type',
	'no handler returned a result',
	'partitionBy returned null',
	'link event ($includeLinks not set)',
	'stream deletion (no $deleted handler)',
];

const ERROR_CODES = ['HANDLER_ERROR', 'RUNTIME_ERROR', 'PARSE_ERROR', 'EMIT_FAILED'];

const ERROR_DESCS = [
	"TypeError: Cannot read property 'cents' of undefined",
	'ReferenceError: x is not defined',
	'SyntaxError: Unexpected token } in JSON at position 12',
	'TypeError: state.count.toUpperCase is not a function',
	'RangeError: Maximum call stack size exceeded',
];

const DATA_TEMPLATES: Record<string, string[]> = {
	OrderPlaced: ['{"cents":4999,"items":3}', '{"cents":1299,"items":1}', '{"cents":8750,"items":5}'],
	OrderShipped: ['{"carrier":"UPS","tracking":"1Z42A"}', '{"carrier":"FedEx","tracking":"F84922"}'],
	PaymentReceived: ['{"cents":4999,"method":"card"}', '{"cents":12000,"method":"ach"}'],
	RefundIssued: ['{"cents":1999,"reason":"return"}'],
	CartUpdated: ['{"itemId":"sku_42","qty":2}', '{"itemId":"sku_19","qty":-1}'],
	UserSignedUp: ['{"email":"alice@example.com"}', '{"email":"bob@example.com"}'],
	UserDeactivated: ['{"reason":"churn"}', '{"reason":"requested"}'],
	InventoryAdjusted: ['{"sku":"sku_42","delta":-1}', '{"sku":"sku_19","delta":4}'],
	SubscriptionRenewed: ['{"plan":"pro","cents":2999}'],
	SubscriptionCancelled: ['{"plan":"pro","reason":"price"}'],
	EmailSent: ['{"to":"alice@example.com","template":"welcome"}'],
	LoginFailed: ['{"ip":"10.0.0.1","attempts":3}'],
};

type Source =
	| { kind: 'all' }
	| { kind: 'streams'; names: string[] }
	| { kind: 'category'; name: string };

type Partitioning = 'none' | 'per-stream' | 'custom';

type Run = {
	name: string;
	source: Source;
	partitioning: Partitioning;
	events: string[];
	engineVersion: number;
	dbVersion: string;
	totalEvents: number;
	processed: number;
	skipped: number;
	errors: number;
	partitionStates: Map<string, { count: number; totalCents: number }>;
	globalState: { count: number; totalCents: number };
};

function pick<T>(arr: readonly T[]): T {
	return arr[Math.floor(Math.random() * arr.length)];
}

function pickN<T>(arr: readonly T[], n: number): T[] {
	const copy = [...arr];
	const out: T[] = [];
	for (let i = 0; i < n && copy.length; i++) {
		const idx = Math.floor(Math.random() * copy.length);
		out.push(copy.splice(idx, 1)[0]);
	}
	return out;
}

function chance(p: number): boolean {
	return Math.random() < p;
}

function range(min: number, max: number): number {
	return min + Math.floor(Math.random() * (max - min + 1));
}

function shortId(): string {
	if (chance(0.65)) return String(range(1, 999));
	return Math.random().toString(36).slice(2, 6);
}

function makeRun(): Run {
	const sourceRoll = Math.random();
	let source: Source;
	if (sourceRoll < 0.4) {
		source = { kind: 'all' };
	} else if (sourceRoll < 0.75) {
		source = { kind: 'category', name: pick(CATEGORIES) };
	} else {
		const cat = pick(CATEGORIES);
		const count = range(2, 3);
		const names: string[] = [];
		for (let i = 0; i < count; i++) names.push(`${cat}-${shortId()}`);
		source = { kind: 'streams', names };
	}
	const partRoll = Math.random();
	const partitioning: Partitioning =
		partRoll < 0.55 ? 'per-stream' : partRoll < 0.85 ? 'none' : 'custom';
	return {
		name: pick(PROJECTION_NAMES),
		source,
		partitioning,
		events: pickN(EVENT_TYPES, range(2, 4)),
		engineVersion: pick([1, 2]),
		dbVersion: pick(DB_VERSIONS),
		totalEvents: range(8, 24),
		processed: 0,
		skipped: 0,
		errors: 0,
		partitionStates: new Map(),
		globalState: { count: 0, totalCents: 0 },
	};
}

const tok = {
	plain: (text: string): Token => ({ kind: 'plain', text }),
	pipe: (text: string): Token => ({ kind: 'pipe', text }),
	label: (text: string): Token => ({ kind: 'label', text }),
	heading: (text: string): Token => ({ kind: 'heading', text }),
	processed: (text: string): Token => ({ kind: 'processed', text }),
	skipped: (text: string): Token => ({ kind: 'skipped', text }),
	errStatus: (text: string): Token => ({ kind: 'errStatus', text }),
	errDetail: (text: string): Token => ({ kind: 'errDetail', text }),
};

// CLI prefix glyphs. Indent is 3, matching cli/cmd/output_text.go.
const PIPE = '│  ';
const TEE = '├ ';
const CORNER = '╰ ';
const SUB_MID = '│  │  ';
const SUB_END = '│  ╵  ';
const POST_CORNER = '   ';

function detail(label: string, value: string): Token[] {
	return [tok.label(`${label}:`), tok.plain(` ${value}`)];
}

// Pauses encode the real CLI rhythm: lines within a single processing
// step land essentially together, then a beat before the next event,
// then a long rest between projection runs.
const PAUSE_TIGHT = 100; // within a block; faithful to real CLI output
const PAUSE_HEADER_END = 900; // header → first event
const PAUSE_BEFORE_STATE = 350; // "N events processed" → state body

function pauseBetweenEvents(): number {
	return range(500, 1400);
}

function line(tokens: Token[], pauseAfterMs: number = PAUSE_TIGHT): Line {
	return { tokens, pauseAfterMs };
}

function blank(pauseAfterMs: number = PAUSE_TIGHT): Line {
	return { tokens: [], pauseAfterMs };
}

function jsonShape(eventType: string): string {
	return pick(DATA_TEMPLATES[eventType] ?? ['{"value":42}']);
}

function pickStream(run: Run): string {
	if (run.source.kind === 'streams') return pick(run.source.names);
	if (run.source.kind === 'category') return `${run.source.name}-${shortId()}`;
	return `${pick(CATEGORIES)}-${shortId()}`;
}

function partitionFor(run: Run, stream: string): string | null {
	if (run.partitioning === 'per-stream') return stream;
	if (run.partitioning === 'custom') return `key-${range(1, 5)}`;
	return null;
}

function* header(run: Run, isFirstRun: boolean): Generator<Line> {
	if (!isFirstRun) yield blank();
	yield line([tok.heading(run.name)]);

	let sourceValue: string;
	if (run.source.kind === 'all') sourceValue = '$all';
	else if (run.source.kind === 'category') sourceValue = `category ${run.source.name}`;
	else sourceValue = `streams ${run.source.names.join(', ')}`;
	yield line(detail('Source', sourceValue));

	if (run.partitioning === 'per-stream') {
		yield line(detail('Partitioning', 'per stream'));
	} else if (run.partitioning === 'custom') {
		yield line(detail('Partitioning', 'custom key'));
	}

	yield line(detail('Events', run.events.join(', ')));
	yield line(detail('Engine', `v${run.engineVersion}`));
	yield line(detail('DB version', run.dbVersion), PAUSE_HEADER_END);
}

function* emits(): Generator<Line> {
	const count = range(1, 2);
	for (let i = 0; i < count; i++) {
		yield line([tok.pipe(TEE), tok.processed('emitted')]);
		const emitStream = `${pick(CATEGORIES)}-${shortId()}`;
		const emitType = pick(EVENT_TYPES);
		yield line([tok.pipe(SUB_MID), ...detail('stream', emitStream)]);
		yield line([tok.pipe(SUB_MID), ...detail('type', emitType)]);
		yield line([tok.pipe(SUB_END), ...detail('data', jsonShape(emitType))]);
	}
}

function* event(run: Run): Generator<Line> {
	yield blank();
	const seq = run.processed + run.skipped + run.errors + 1;
	const stream = pickStream(run);
	const eventType = pick(run.events);

	yield line([tok.heading(`${seq}@${stream}`)]);
	yield line([tok.pipe(PIPE), ...detail('type', eventType)]);
	yield line([tok.pipe(PIPE), ...detail('data', jsonShape(eventType))]);
	if (chance(0.55)) {
		yield line([tok.pipe(PIPE), ...detail('metadata', `{"correlationId":"${shortId()}"}`)]);
	}

	const roll = Math.random();

	if (roll < 0.08) {
		run.skipped++;
		yield line([tok.pipe(CORNER), tok.skipped('skipped')]);
		yield line([tok.plain(POST_CORNER), ...detail('reason', pick(SKIP_REASONS))], pauseBetweenEvents());
	} else if (roll < 0.13) {
		run.errors++;
		yield line([tok.pipe(CORNER), tok.errStatus(pick(ERROR_CODES))]);
		yield line([tok.plain(POST_CORNER), tok.errDetail(pick(ERROR_DESCS))], pauseBetweenEvents());
	} else {
		run.processed++;
		if (chance(0.25)) yield* emits();
		const partition = partitionFor(run, stream);
		yield line([tok.pipe(CORNER), tok.processed('processed')]);
		if (partition) {
			yield line([tok.plain(POST_CORNER), ...detail('partition', partition)]);
		}
		const stateKey = partition ?? '__global__';
		let state = partition ? run.partitionStates.get(stateKey) : run.globalState;
		if (!state && partition) {
			state = { count: 0, totalCents: 0 };
			run.partitionStates.set(stateKey, state);
		}
		state!.count++;
		state!.totalCents += range(100, 5000);
		const stateJson = `{"count":${state!.count},"totalCents":${state!.totalCents}}`;
		yield line([tok.plain(POST_CORNER), ...detail('state', stateJson)], pauseBetweenEvents());
	}
}

function* summary(run: Run): Generator<Line> {
	yield blank();
	const tokens: Token[] = [tok.processed(String(run.processed)), tok.plain(' events processed')];
	if (run.errors > 0) {
		tokens.push(tok.plain(', '), tok.errStatus(String(run.errors)), tok.plain(' errors'));
	}
	yield line(tokens, PAUSE_BEFORE_STATE);
	yield blank();

	const stateLines: Line[] = [];
	if (run.partitioning === 'per-stream' || run.partitioning === 'custom') {
		const entries = Array.from(run.partitionStates.entries()).slice(0, 3);
		for (const [partition, state] of entries) {
			stateLines.push(line([tok.heading(partition)]));
			const stateJson = `{"count":${state.count},"totalCents":${state.totalCents}}`;
			stateLines.push(line([tok.plain(POST_CORNER), ...detail('state', stateJson)]));
		}
	} else if (run.globalState.count > 0) {
		const state = run.globalState;
		const stateJson = `{"count":${state.count},"totalCents":${state.totalCents}}`;
		stateLines.push(line(detail('State', stateJson)));
	}
	if (stateLines.length > 0) {
		stateLines[stateLines.length - 1].pauseAfterMs = pauseBetweenEvents();
	}
	for (const l of stateLines) yield l;
}

export function* createStream(): Generator<Line> {
	let isFirst = true;
	while (true) {
		const run = makeRun();
		yield* header(run, isFirst);
		isFirst = false;
		for (let i = 0; i < run.totalEvents; i++) yield* event(run);
		yield* summary(run);
	}
}
