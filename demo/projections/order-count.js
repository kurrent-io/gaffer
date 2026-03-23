fromCategory('order')
  .foreachStream()
  .when({
    $init: function() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced: function(state, event) {
      state.count++;
      state.totalCents += event.data.cents;
      return state;
    },
    OrderShipped: function(state, event) {
      state.shipped = true;
      return state;
    }
  })
