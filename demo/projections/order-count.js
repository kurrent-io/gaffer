fromCategory('order')
  .foreachStream()
  .when({
    $init() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced(state, event) {
      state.count++;
      state.totalCents += event.data.cents;
      return state;
    },
    OrderShipped(state, event) {
      state.shipped = true;
      return state;
    }
  })
