# Demo

Example gaffer project for manual testing.

## Run

```sh
just cli run -- -C demo dev order-count --events fixtures/orders.json
```

Or build the CLI first:

```sh
just cli build
cd demo
../cli/gaffer dev order-count --events fixtures/orders.json
```
