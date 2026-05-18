import { dotnet } from './_framework/dotnet.js'

const log = (msg) => {
	const out = document.getElementById('out');
	out.textContent += msg + '\n';
	console.log(msg);
};

const t0 = performance.now();

const { getAssemblyExports, getConfig } = await dotnet
	.withDiagnosticTracing(false)
	.create();

const config = getConfig();
const exports = await getAssemblyExports(config.mainAssemblyName);
const tInit = performance.now();
log(`runtime ready in ${(tInit - t0).toFixed(0)}ms`);

const projection = `
	fromAll().when({
		$init() { return { count: 0, total: 0 }; },
		OrderPlaced(s, e) { s.count++; s.total += e.body.amount; return s; }
	})
`;

exports.GafferWasm.CreateSession(projection);
const tCreated = performance.now();
log(`session created in ${(tCreated - tInit).toFixed(1)}ms`);

for (let i = 1; i <= 3; i++) {
	exports.GafferWasm.Feed('OrderPlaced', `order-${i}`, JSON.stringify({ amount: i * 10 }));
}
const tFed = performance.now();
log(`fed 3 events in ${(tFed - tCreated).toFixed(1)}ms`);

const state = exports.GafferWasm.GetState();
log(`state: ${state}`);

await dotnet.run();
