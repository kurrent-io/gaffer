using System.Text.Json;
using Gaffer.Runtime.Events;

namespace Gaffer.Runtime.Tests;

public class UserLoginProjectionTests
{
    private const int LockoutThreshold = 3;

    private const string Source = $$"""
        fromCategory("user")
        .foreachStream()
        .when({
            $init: function() {
                return { successfulLogins: 0, failedLogins: 0, lockedOut: false };
            },
            UserRegistered: function(s, e) {
                return s;
            },
            UserLoggedIn: function(s, e) {
                s.successfulLogins++;
                s.failedLogins = 0;
                return s;
            },
            UserLoginFailed: function(s, e) {
                s.failedLogins++;
                if (s.failedLogins >= 3 && !s.lockedOut) {
                    s.lockedOut = true;
                    emit("lockouts", "UserLockedOut", { userId: e.streamId });
                }
                return s;
            },
            UserLockedOut: function(s, e) {
                s.lockedOut = true;
                return s;
            }
        });
        """;

    private static ProjectionEvent Evt(string stream, string type, object? data = null) => new()
    {
        EventType = type,
        StreamId = stream,
        Data = data != null ? JsonSerializer.Serialize(data) : "{}",
    };

    [Fact]
    public void Happy_user_only_successful_logins()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(Source);
        session.OnEmit = e => emitted.Add(e);

        session.Feed(Evt("user-alice", "UserRegistered", new { username = "alice" }));
        session.Feed(Evt("user-alice", "UserLoggedIn"));
        session.Feed(Evt("user-alice", "UserLoggedIn"));
        session.Feed(Evt("user-alice", "UserLoggedIn"));

        var state = session.GetState("user-alice")!;
        Assert.Contains("\"successfulLogins\":3", state);
        Assert.Contains("\"failedLogins\":0", state);
        Assert.Contains("\"lockedOut\":false", state);
        Assert.Empty(emitted);
    }

    [Fact]
    public void Failed_attempts_then_recovery()
    {
        using var session = new ProjectionSession(Source);

        session.Feed(Evt("user-bob", "UserRegistered", new { username = "bob" }));
        session.Feed(Evt("user-bob", "UserLoginFailed", new { reason = "wrong_password" }));
        session.Feed(Evt("user-bob", "UserLoginFailed", new { reason = "wrong_password" }));
        session.Feed(Evt("user-bob", "UserLoggedIn"));

        var state = session.GetState("user-bob")!;
        Assert.Contains("\"successfulLogins\":1", state);
        Assert.Contains("\"failedLogins\":0", state);
        Assert.Contains("\"lockedOut\":false", state);
    }

    [Fact]
    public void Lockout_after_threshold()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(Source);
        session.OnEmit = e => emitted.Add(e);

        session.Feed(Evt("user-charlie", "UserRegistered", new { username = "charlie" }));
        for (var i = 0; i < LockoutThreshold; i++)
            session.Feed(Evt("user-charlie", "UserLoginFailed", new { reason = "wrong_password" }));

        var state = session.GetState("user-charlie")!;
        Assert.Contains("\"failedLogins\":3", state);
        Assert.Contains("\"lockedOut\":true", state);

        Assert.Single(emitted);
        Assert.Equal("lockouts", emitted[0].StreamId);
        Assert.Equal("UserLockedOut", emitted[0].EventType);
    }

    [Fact]
    public void Lockout_emits_only_once()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(Source);
        session.OnEmit = e => emitted.Add(e);

        session.Feed(Evt("user-dave", "UserRegistered"));
        for (var i = 0; i < LockoutThreshold + 3; i++)
            session.Feed(Evt("user-dave", "UserLoginFailed", new { reason = "wrong_password" }));

        Assert.Single(emitted);
    }

    [Fact]
    public void Mixed_pattern_fail_succeed_then_lockout()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(Source);
        session.OnEmit = e => emitted.Add(e);

        session.Feed(Evt("user-eve", "UserRegistered"));
        session.Feed(Evt("user-eve", "UserLoginFailed"));
        session.Feed(Evt("user-eve", "UserLoggedIn"));
        // failedLogins reset to 0, need 3 more to lock out
        for (var i = 0; i < LockoutThreshold; i++)
            session.Feed(Evt("user-eve", "UserLoginFailed"));

        var state = session.GetState("user-eve")!;
        Assert.Contains("\"successfulLogins\":1", state);
        Assert.Contains("\"failedLogins\":3", state);
        Assert.Contains("\"lockedOut\":true", state);
        Assert.Single(emitted);
    }

    [Fact]
    public void Multiple_users_independent_state()
    {
        using var session = new ProjectionSession(Source);

        session.Feed(Evt("user-alice", "UserRegistered"));
        session.Feed(Evt("user-bob", "UserRegistered"));
        session.Feed(Evt("user-alice", "UserLoggedIn"));
        session.Feed(Evt("user-bob", "UserLoginFailed"));
        session.Feed(Evt("user-alice", "UserLoggedIn"));
        session.Feed(Evt("user-bob", "UserLoginFailed"));

        var alice = session.GetState("user-alice")!;
        Assert.Contains("\"successfulLogins\":2", alice);
        Assert.Contains("\"failedLogins\":0", alice);

        var bob = session.GetState("user-bob")!;
        Assert.Contains("\"successfulLogins\":0", bob);
        Assert.Contains("\"failedLogins\":2", bob);
    }

    [Fact]
    public void Many_users_deterministic_scenarios()
    {
        var emitted = new List<EmittedEvent>();
        using var session = new ProjectionSession(Source);
        session.OnEmit = e => emitted.Add(e);

        var scenarios = GenerateScenarios(20);

        foreach (var scenario in scenarios)
            foreach (var evt in scenario.Events)
                session.Feed(evt);

        foreach (var scenario in scenarios)
        {
            var state = session.GetState(scenario.UserId);
            Assert.NotNull(state);
            Assert.Contains($"\"successfulLogins\":{scenario.ExpectedSuccessful}", state);
            Assert.Contains($"\"failedLogins\":{scenario.ExpectedFailed}", state);
            Assert.Contains($"\"lockedOut\":{(scenario.ExpectedLockedOut ? "true" : "false")}", state);
        }

        var lockedOutUsers = scenarios.Count(s => s.ExpectedLockedOut);
        Assert.Equal(lockedOutUsers, emitted.Count(e => e.EventType == "UserLockedOut"));
    }

    private record Scenario(string UserId, List<ProjectionEvent> Events, int ExpectedSuccessful, int ExpectedFailed, bool ExpectedLockedOut);

    private static List<Scenario> GenerateScenarios(int userCount)
    {
        var rng = new Random(42);
        var scenarios = new List<Scenario>();

        for (var i = 0; i < userCount; i++)
        {
            var userId = $"user-u{i:D4}";
            var events = new List<ProjectionEvent>();
            var successful = 0;
            var consecutiveFails = 0;
            var lockedOut = false;

            events.Add(Evt(userId, "UserRegistered", new { username = $"u{i:D4}" }));

            switch (i % 5)
            {
                case 0: // Happy user
                    for (var j = 0; j < 3 + rng.Next(3); j++)
                    {
                        events.Add(Evt(userId, "UserLoggedIn"));
                        successful++;
                        consecutiveFails = 0;
                    }
                    break;
                case 1: // Some fails then recovery
                    events.Add(Evt(userId, "UserLoginFailed"));
                    events.Add(Evt(userId, "UserLoginFailed"));
                    events.Add(Evt(userId, "UserLoggedIn"));
                    successful++;
                    consecutiveFails = 0;
                    break;
                case 2: // Locked out
                    for (var j = 0; j < LockoutThreshold; j++)
                    {
                        events.Add(Evt(userId, "UserLoginFailed"));
                        consecutiveFails++;
                    }
                    lockedOut = true;
                    break;
                case 3: // Locked out + extra fails
                    for (var j = 0; j < LockoutThreshold + 1; j++)
                    {
                        events.Add(Evt(userId, "UserLoginFailed"));
                        consecutiveFails++;
                        if (consecutiveFails == LockoutThreshold) lockedOut = true;
                    }
                    break;
                case 4: // Fail, succeed, then lockout
                    events.Add(Evt(userId, "UserLoginFailed"));
                    events.Add(Evt(userId, "UserLoggedIn"));
                    successful++;
                    consecutiveFails = 0;
                    for (var j = 0; j < LockoutThreshold; j++)
                    {
                        events.Add(Evt(userId, "UserLoginFailed"));
                        consecutiveFails++;
                    }
                    lockedOut = true;
                    break;
            }

            scenarios.Add(new Scenario(userId, events, successful, consecutiveFails, lockedOut));
        }

        return scenarios;
    }
}
