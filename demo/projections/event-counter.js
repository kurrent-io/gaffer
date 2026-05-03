fromAll()
  .partitionBy(function (event) {
    return event.eventType;
  })
  .when({
    $init() {
      return { count: 0 };
    },
    $UserCreated(state, event) {
      log("created!");
      state.count++;
      log("bumped!");
      emit(
        "notifications",
        "OrderReceived",
        {
          orderId: event.streamId,
          cents: state.count,
        },
        { source: "event-counter" },
      );
      log("emitted!");
      log("okay!");
      return state;
    },
    $ProjectionCreated(state, event) {
      state.count++;
      return state;
    },
    $ProjectionUpdated(state, event) {
      state.count++;
      return state;
    },
    ServerInfo(state, event) {
      state.count++;
      return state;
    },
    $any(state, event) {
      state.count++;
      return state;
    },
  });
