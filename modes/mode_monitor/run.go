// Package mode_monitor watches an Active Directory domain over LDAP and reports what
// changes in it, until the process is interrupted: objects created, objects deleted,
// and the attribute values of existing objects.
//
// It works by snapshotting every object in scope, then comparing the latest snapshot
// with the previous one. A change is therefore seen at the next query after it lands,
// and the refresh rate is bounded by how long a full enumeration of the search bases
// takes.
package mode_monitor

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/Manticore/network/ldap"

	"github.com/TheManticoreProject/manticore-ldapmonitor/config"
	"github.com/TheManticoreProject/manticore-ldapmonitor/directory"
	"github.com/TheManticoreProject/manticore-ldapmonitor/utils"
)

// randomDelayLowerBoundMs and randomDelayUpperBoundMs bound, in milliseconds, the
// delay picked before each query when --randomize-delay is set. They are counted in
// milliseconds rather than as durations so the span stays well inside an int on a
// 32-bit build, where a nanosecond span of several seconds would overflow.
const (
	randomDelayLowerBoundMs = 1000
	randomDelayUpperBoundMs = 5000
)

// Run monitors the directory until the process is interrupted.
//
// Parameters:
//
//	cfg (config.Config): The configuration of the run: where to connect, as whom,
//	  what to watch and how often.
//
// Returns:
//
//	nil when the monitoring was interrupted, an error if connecting to the domain
//	controller or querying it failed.
func Run(cfg config.Config) error {
	utils.AnnounceConnection(cfg)

	ldapSession, err := utils.NewSession(cfg)
	if err != nil {
		return err
	}
	defer ldapSession.Close()

	utils.AnnounceIdentity(cfg.Credentials)

	searchBases, err := utils.ResolveSearchBases(ldapSession, cfg.SearchBase)
	if err != nil {
		return err
	}
	cfg.Scope.SearchBases = searchBases
	utils.AnnounceSearchBases("Monitored search bases", searchBases)

	// Ctrl-C has to end the run cleanly rather than kill it mid-query, so the log
	// file written with --logfile is flushed and closed by the caller's deferred
	// CloseLogFile. It is installed before the first snapshot so that interrupting
	// the tool behaves the same way at every point of the run.
	stopRequested := watchForInterrupt()

	previousSnapshot, err := directory.TakeSnapshot(ldapSession, cfg.Scope, cfg.Debug)
	if err != nil {
		return err
	}
	logger.Print(fmt.Sprintf("[>] Objects in the initial snapshot: \x1b[93m%d\x1b[0m.", len(previousSnapshot)))

	ignored := directory.IgnoredAttributes(cfg.Reporting.IgnoreUserLogon)

	logger.Print("[>] Listening for LDAP changes ...")
	for {
		if interrupted(stopRequested) {
			logger.Print("[>] Interrupted, stopping.")
			return nil
		}

		delay := nextDelay(cfg.Monitoring)
		if cfg.Debug {
			logger.Debug(fmt.Sprintf("Waiting %s before the next query", delay))
		}

		select {
		case <-stopRequested:
			logger.Print("[>] Interrupted, stopping.")
			return nil
		case <-time.After(delay):
		}

		currentSnapshot, err := takeSnapshotReconnecting(ldapSession, cfg)
		if err != nil {
			return err
		}

		for _, change := range directory.Diff(previousSnapshot, currentSnapshot, ignored) {
			directory.Render(change)
		}

		previousSnapshot = currentSnapshot
	}
}

// watchForInterrupt arranges for the first interrupt to request a clean stop, and
// for the next one to kill the process outright.
//
// An enumeration cannot be cancelled halfway through, and on a large domain it runs
// for tens of seconds. Merely capturing the signal would leave the operator holding
// a tool that ignores Ctrl-C for that whole time, with no way out short of SIGKILL:
// so the default signal behaviour is restored as soon as the first signal arrives,
// and a second Ctrl-C terminates immediately.
//
// Returns:
//
//	A channel that is closed when a clean stop has been requested.
func watchForInterrupt() <-chan struct{} {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	stopRequested := make(chan struct{})
	go func() {
		<-signals
		signal.Stop(signals)
		logger.Print("[>] Interrupt received, stopping after the running query (interrupt again to abort now).")
		close(stopRequested)
	}()

	return stopRequested
}

// interrupted reports whether a clean stop has already been requested, without
// waiting for one.
//
// Parameters:
//
//	stopRequested (<-chan struct{}): The channel returned by watchForInterrupt.
//
// Returns:
//
//	True when a stop has been requested, false otherwise.
func interrupted(stopRequested <-chan struct{}) bool {
	select {
	case <-stopRequested:
		return true
	default:
		return false
	}
}

// takeSnapshotReconnecting takes a snapshot, reconnecting once and retrying if the
// query fails.
//
// A monitoring run is meant to last hours, over which the domain controller will
// drop the connection at some point. That is not a reason to abandon the run, so a
// failed query is retried once on a fresh connection, and only a second failure ends
// the run.
//
// Parameters:
//
//	ldapSession (*ldap.Session): The LDAP session to query, reconnected in place on failure.
//	cfg (config.Config): The configuration of the run.
//
// Returns:
//
//	The snapshot, or an error if the query failed twice.
func takeSnapshotReconnecting(ldapSession *ldap.Session, cfg config.Config) (directory.Snapshot, error) {
	snapshot, err := directory.TakeSnapshot(ldapSession, cfg.Scope, cfg.Debug)
	if err == nil {
		return snapshot, nil
	}

	logger.Warn(fmt.Sprintf("Query failed (%s), reconnecting to %s ...", err, cfg.Network.DomainController))

	connected, reconnectErr := ldapSession.ReConnect()
	if !connected {
		return nil, fmt.Errorf("error reconnecting to LDAP server: %w", reconnectErr)
	}

	snapshot, err = directory.TakeSnapshot(ldapSession, cfg.Scope, cfg.Debug)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// nextDelay returns how long to wait before the next query.
//
// Parameters:
//
//	monitoring (config.Monitoring): The monitoring options of the run.
//
// Returns:
//
//	The delay to wait.
func nextDelay(monitoring config.Monitoring) time.Duration {
	if monitoring.RandomizeDelay {
		span := randomDelayUpperBoundMs - randomDelayLowerBoundMs + 1
		return time.Duration(randomDelayLowerBoundMs+rand.IntN(span)) * time.Millisecond
	}
	return time.Duration(monitoring.TimeDelay) * time.Second
}
