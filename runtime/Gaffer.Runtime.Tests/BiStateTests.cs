using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class BiStateTests {
	private const string AccountBalancerSource = """
        options({ biState: true });

        fromCategory("transaction")
            .partitionBy(function(e) {
                if (e.eventType === "header") return "description";
                if (e.body.accountId.startsWith("ESDBB"))
                    return e.body.accountId;
                return undefined;
            })
            .when({
                $init: function() {
                    return { balance: 0 };
                },
                $initShared: function() {
                    return {
                        numberOfAccounts: 0,
                        totalBalance: 0
                    };
                },
                $created: function(s, e) {
                    if (e.partition !== "description") {
                        s[1].numberOfAccounts++;
                    } else {
                        s[0] = null;
                    }
                },
                "header": function(s, e) {
                    s[1].description = e.body.description;
                },
                "credit": function(s, e) {
                    if (e.partition === "") return {};
                    s[0].balance += e.body.amount;
                    s[0].credit = e.body.amount;
                    s[0].debit = undefined;
                    s[0].description = s[1].description;
                    s[1].totalBalance += e.body.amount;
                    return s;
                },
                "debit": function(s, e) {
                    if (e.partition === "") return {};
                    s[0].balance -= e.body.amount;
                    s[0].credit = undefined;
                    s[0].debit = e.body.amount;
                    s[0].description = s[1].description;
                    s[1].totalBalance -= e.body.amount;
                    return s;
                }
            });
        """;

	[Fact]
	public void BiState_source_definition() {
		using var session = new ProjectionSession(AccountBalancerSource);
		Assert.True(session.Sources.IsBiState);
		Assert.True(session.Sources.ByCustomPartitions);
	}

	[Fact]
	public void Simple_bistate_shared_state() {
		var stateChanges = new List<(string partition, string? state)>();
		using var session = new ProjectionSession("""
            options({ biState: true });
            fromAll().when({
                $init: function() { return { count: 0 }; },
                $initShared: function() { return { total: 0 }; },
                Added: function(s, e) {
                    s[0].count++;
                    s[1].total += e.data.amount;
                    return s;
                }
            })
        """);
		session.OnStateChanged = (p, s) => stateChanges.Add((p, s));

		session.Feed(new ProjectionEvent { EventType = "Added", StreamId = "s-1", Data = """{"amount":10}""" });
		session.Feed(new ProjectionEvent { EventType = "Added", StreamId = "s-1", Data = """{"amount":20}""" });

		// Per-partition state (s[0]) has count
		var partitionState = session.GetState(null);
		Assert.NotNull(partitionState);
		Assert.Contains("\"count\":2", partitionState);

		// Shared state (s[1]) has total
		var sharedState = session.GetSharedState();
		Assert.NotNull(sharedState);
		Assert.Contains("\"total\":30", sharedState);
	}

	[Fact]
	public void Account_balancer_full_spec() {
		using var session = new ProjectionSession(AccountBalancerSource);

		// Transaction 1: transfer from alice
		session.Feed(new ProjectionEvent {
			EventType = "header",
			StreamId = "transaction-1",
			Data = """{"description":"transfer from alice"}""",
		});
		session.Feed(new ProjectionEvent {
			EventType = "credit",
			StreamId = "transaction-1",
			Data = """{"accountId":"ESDBB-01","amount":1000}""",
		});

		// Transaction 2: transfer to savings
		session.Feed(new ProjectionEvent {
			EventType = "header",
			StreamId = "transaction-2",
			Data = """{"description":"transfer to savings"}""",
		});
		session.Feed(new ProjectionEvent {
			EventType = "debit",
			StreamId = "transaction-2",
			Data = """{"accountId":"ESDBB-01","amount":300}""",
		});
		session.Feed(new ProjectionEvent {
			EventType = "credit",
			StreamId = "transaction-2",
			Data = """{"accountId":"ESDBB-S01","amount":300}""",
		});

		// Transaction 3: bill payment
		session.Feed(new ProjectionEvent {
			EventType = "header",
			StreamId = "transaction-3",
			Data = """{"description":"bill payment"}""",
		});
		session.Feed(new ProjectionEvent {
			EventType = "debit",
			StreamId = "transaction-3",
			Data = """{"accountId":"ESDBB-01","amount":150}""",
		});

		// Verify ESDBB-01: balance=550, debit=150
		var esdbb01 = session.GetState("ESDBB-01");
		Assert.NotNull(esdbb01);
		Assert.Contains("\"balance\":550", esdbb01);
		Assert.Contains("\"debit\":150", esdbb01);

		// Verify ESDBB-S01: balance=300, credit=300
		var esdbbS01 = session.GetState("ESDBB-S01");
		Assert.NotNull(esdbbS01);
		Assert.Contains("\"balance\":300", esdbbS01);
		Assert.Contains("\"credit\":300", esdbbS01);

		// Verify shared state: numberOfAccounts=2, totalBalance=850
		var shared = session.GetSharedState();
		Assert.NotNull(shared);
		Assert.Contains("\"numberOfAccounts\":2", shared);
		Assert.Contains("\"totalBalance\":850", shared);
	}
}
