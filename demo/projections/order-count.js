fromCategory('order')
  .foreachStream()
  .when({
    $init: function() {
      return { count: 0, totalAmount: 0 };
    },
    OrderPlaced: function(state, event) {
      state.count++;
      state.totalAmount += event.data.amount;
      return state;
    },
    OrderShipped: function(state, event) {
      state.shipped = true;
      return state;
    }
  })
