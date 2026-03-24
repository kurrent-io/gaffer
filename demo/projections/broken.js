fromCategory('order')
  .foreachStream()
  .when({
    $init: function() {
      return { count: 0 }
    },
    OrderPlaced: function(state, event) {
      state.count++
      return state
    // missing closing brace
  })
