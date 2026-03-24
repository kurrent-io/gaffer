fromCategory('order')
  .foreachStream()
  .when({
    $init: function() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced: function(state, event) {
      log("Processing order: " + event.data.item);
      state.count++;
      state.totalCents += event.data.cents;
      emit("notifications", "OrderReceived", { item: event.data.item, stream: event.streamId });
      return state;
    },
    OrderShipped: function(state, event) {
      state.shipped = true;
      linkTo("shipped-orders", event);
      return state;
    },
    OrderFailed: function(state, event) {
      throw new Error("Cannot process failed order: " + event.data.reason);
    }
  })
