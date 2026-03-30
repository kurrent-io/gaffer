fromAll()
  .foreachStream()
  .when({
    $init() {
      return { count: 0 };
    },
    $initShared() {
      return { total: 0 };
    },
    $any(state, shared, event) {
      state.count++;
      shared.total++;
      return [state, shared];
    },
    // $UserCreated([state, shared], event) {
    //   state.count++;
    //   shared.total++;
    //   return [state, shared];
    // },
    // $ProjectionCreated([state, shared], event) {
    //   state.count++;
    //   shared.total++;
    //   return [state, shared];
    // },
    // $ProjectionUpdated([state, shared], event) {
    //   state.count++;
    //   shared.total++;
    //   return [state, shared];
    // },
    // ServerInfo([state, shared], event) {
    //   state.count++;
    //   shared.total++;
    //   return [state, shared];
    // },
  });
