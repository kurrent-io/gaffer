fromCategory("order")
  .foreachStream()
  .when({
    $init() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced(state, event) {
      log("Processing order: " + event.body.item);
      state.count++;
      state.totalCents += event.body.cents;
      emit(
        "notifications",
        "OrderReceived",
        {
          orderId: event.streamId,
          item: event.body.item,
          cents: event.body.cents,
        },
        { source: "order-notifications" },
      );
      log(
        "Order received: " + event.body.item + " (" + event.body.cents + "c)",
      );
      return state;
    },
    OrderShipped(state, event) {
      state.shipped = true;
      state.trackingId = event.body.trackingId;
      linkTo("shipped-orders", event, { reason: "shipped" });
      return state;
    },
    OrderFailed(state, event) {
      throw new Error("Cannot process failed order: " + event.body.reason);
    },
  });
