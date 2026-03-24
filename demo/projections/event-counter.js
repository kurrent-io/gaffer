fromAll()
  .partitionBy(function(event) {
    return event.eventType;
  })
  .when({
    $init: function() {
      return { count: 0 };
    },
    $UserCreated: function(state, event) {
      state.count++;
      return state;
    },
    $ProjectionCreated: function(state, event) {
      state.count++;
      return state;
    },
    $ProjectionUpdated: function(state, event) {
      state.count++;
      return state;
    },
    ServerInfo: function(state, event) {
      state.count++;
      return state;
    }
  })
