// Test helper: applies the SQL migrations to a fresh D1 database.
//
// `?raw` lets Vite load the .sql file as a string at bundle time, so the
// tests don't need filesystem access at runtime (they run inside the
// Workers isolate, not Node). The string is split into statements and
// applied via D1's prepared-statement API; D1.exec() does not reliably
// support multi-statement SQL, but a sequence of prepared single
// statements does.
//
// CAVEAT: the splitter is naive - it strips full-line `--` comments and
// splits on `;`. It does NOT handle `;` inside string literals, `BEGIN
// ... END` blocks (CREATE TRIGGER), or `/* ... */` block comments.
// Future migrations that need any of those will need a real SQL parser
// (or just keep migrations to plain CREATE / ALTER / INDEX statements).

import initSql from "../migrations/0001_init.sql?raw";

export async function applyMigrations(db: D1Database): Promise<void> {
	const statements = initSql
		.split(/\n/)
		.map((line: string) => line.replace(/^\s*--.*$/, "")) // strip comments
		.join("\n")
		.split(";")
		.map((s: string) => s.trim())
		.filter((s: string) => s.length > 0);

	for (const stmt of statements) {
		await db.prepare(stmt).run();
	}
}

export async function resetTables(db: D1Database): Promise<void> {
	await db.batch([
		db.prepare("DELETE FROM session_by_user"),
		db.prepare("DELETE FROM session_by_run"),
		db.prepare("DELETE FROM merged_pairs"),
	]);
}
