fromCategory("order")
  .foreachStream()
  .when({
    $init() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced(state, event) {
      log("Processing order: " + event.data.item);
      state.count++;
      state.totalCents += event.data.cents;
      emit(
        "notifications",
        "OrderReceived",
        {
          orderId: event.streamId,
          item: event.data.item,
          cents: event.data.cents,
        },
        { source: "order-notifications" },
      );
      log(
        "Order received: " + event.data.item + " (" + event.data.cents + "c)",
      );
      return state;
    },
    OrderShipped(state, event) {
      state.shipped = true;
      state.trackingId = event.data.trackingId;
      linkTo("shipped-orders", event, { reason: "shipped" });
      return state;
    },
    OrderFailed(state, event) {
      throw new Error("Cannot process failed order: " + event.data.reason);
    },
  });
