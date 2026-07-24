import eslint from "@eslint/js";
import solid from "eslint-plugin-solid/configs/typescript";
import tseslint from "typescript-eslint";

export default [
	eslint.configs.recommended,
	...tseslint.configs.recommended,
	{
		ignores: ["dist/", "tsserver-plugin/dist/"],
	},
	{
		rules: {
			"@typescript-eslint/no-unused-vars": [
				"error",
				{ argsIgnorePattern: "^_" },
			],
			"@typescript-eslint/no-non-null-assertion": "error",
		},
	},
	// Solid rules apply only to the webview source; the extension host is not
	// Solid and would trip the reactivity/react-prop rules.
	{
		...solid,
		files: ["src/webviews/**/*.{ts,tsx}"],
		languageOptions: {
			...solid.languageOptions,
			parserOptions: {
				...solid.languageOptions?.parserOptions,
				project: "./tsconfig.webviews.json",
			},
		},
	},
	{
		files: ["src/webviews/**/*.{ts,tsx}"],
		rules: {
			// Solid's `let el; <div ref={el}>` assigns el through the compiler;
			// the static rule can't see the write.
			"no-unassigned-vars": "off",
		},
	},
];
