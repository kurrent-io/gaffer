fromCategory('order')
  .foreachStream()
  .when({
    $init() {
      return { count: 0 }
    },
    OrderPlaced(state, event) {
      state.count++
      return state
    // missing closing brace
  })
