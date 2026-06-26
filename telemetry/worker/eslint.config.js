import eslint from "@eslint/js";
import tseslint from "typescript-eslint";

export default [
	eslint.configs.recommended,
	...tseslint.configs.recommended,
	{
		ignores: ["dist/", ".wrangler/", "src/notice.gen.ts", "worker-configuration.d.ts"],
	},
	{
		rules: {
			"@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
			"@typescript-eslint/no-non-null-assertion": "error",
		},
	},
];
