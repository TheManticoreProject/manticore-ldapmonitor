// Package monitor watches an Active Directory domain over LDAP and reports what
// changes in it: objects created, objects deleted, and the attribute values of
// existing objects.
//
// It works by snapshotting every object of every monitored search base, then
// comparing the latest snapshot with the previous one. A change is therefore seen at
// the next query after it lands, and the refresh rate is bounded by how long a full
// enumeration of the search bases takes.
package monitor

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/Manticore/network/ldap"
	"github.com/TheManticoreProject/Manticore/windows/credentials"

	"github.com/TheManticoreProject/manticore-ldapmonitor/config"
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
	scheme := "ldap"
	if cfg.Network.LDAP.UseLdaps {
		scheme = "ldaps"
	}
	logger.Print(fmt.Sprintf("[>] Connecting to %s://%s:%d ...", scheme, cfg.Network.DomainController, cfg.Network.LDAP.LDAPPort))

	ldapSession, err := newSession(cfg)
	if err != nil {
		return err
	}
	defer ldapSession.Close()

	if hasSecret(cfg.Credentials) {
		logger.Print(fmt.Sprintf("[+] Authenticated as \x1b[94m%s\\%s\x1b[0m.", cfg.Credentials.GetDomain(), cfg.Credentials.GetUsername()))
	} else {
		logger.Print(fmt.Sprintf("[+] Bound without authentication as \x1b[94m%s\x1b[0m.", cfg.Credentials.GetUsername()))
	}

	searchBases, err := resolveSearchBases(ldapSession, cfg.Monitoring.SearchBase)
	if err != nil {
		return err
	}

	logger.Print(fmt.Sprintf("[>] Monitored search bases (\x1b[93m%d\x1b[0m):", len(searchBases)))
	for index, searchBase := range searchBases {
		branch := "  ├── "
		if index == len(searchBases)-1 {
			branch = "  └── "
		}
		logger.Plain.Print(fmt.Sprintf("%s\x1b[94m%s\x1b[0m", branch, searchBase))
	}

	// Ctrl-C has to end the run cleanly rather than kill it mid-query, so the log
	// file written with --logfile is flushed and closed by the caller's deferred
	// CloseLogFile. It is installed before the first snapshot so that interrupting
	// the tool behaves the same way at every point of the run.
	stopRequested := watchForInterrupt()

	previousSnapshot, err := TakeSnapshot(ldapSession, searchBases, cfg.Debug)
	if err != nil {
		return err
	}
	logger.Print(fmt.Sprintf("[>] Objects in the initial snapshot: \x1b[93m%d\x1b[0m.", len(previousSnapshot)))

	ignored := IgnoredAttributes(cfg.Monitoring.IgnoreUserLogon)

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

		currentSnapshot, err := takeSnapshotReconnecting(ldapSession, searchBases, cfg)
		if err != nil {
			return err
		}

		for _, change := range Diff(previousSnapshot, currentSnapshot, ignored) {
			Render(change)
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
//	searchBases ([]string): The distinguished names to enumerate.
//	cfg (config.Config): The configuration of the run.
//
// Returns:
//
//	The snapshot, or an error if the query failed twice.
func takeSnapshotReconnecting(ldapSession *ldap.Session, searchBases []string, cfg config.Config) (Snapshot, error) {
	snapshot, err := TakeSnapshot(ldapSession, searchBases, cfg.Debug)
	if err == nil {
		return snapshot, nil
	}

	logger.Warn(fmt.Sprintf("Query failed (%s), reconnecting to %s ...", err, cfg.Network.DomainController))

	connected, reconnectErr := ldapSession.ReConnect()
	if !connected {
		return nil, fmt.Errorf("error reconnecting to LDAP server: %w", reconnectErr)
	}

	snapshot, err = TakeSnapshot(ldapSession, searchBases, cfg.Debug)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// newSession creates the LDAP session and binds it to the domain controller.
//
// Parameters:
//
//	cfg (config.Config): The configuration of the run.
//
// Returns:
//
//	A connected LDAP session, or an error if the session could not be created or
//	bound.
func newSession(cfg config.Config) (*ldap.Session, error) {
	ldapSession, err := ldap.NewSession(
		cfg.Network.DomainController,
		cfg.Network.LDAP.LDAPPort,
		cfg.Credentials,
		cfg.Network.LDAP.UseLdaps,
		cfg.Network.LDAP.UseKerberos,
	)
	if err != nil {
		return nil, fmt.Errorf("error creating LDAP session: %w", err)
	}

	// A Kerberos bind to a domain controller named by IP needs the FQDN in the SPN.
	// Set before connecting; it is a no-op for the non-Kerberos and empty cases.
	if cfg.Network.LDAP.UseKerberos && cfg.Network.LDAP.SPNHostname != "" {
		ldapSession.SetKerberosSPNHostname(cfg.Network.LDAP.SPNHostname)
	}

	// A domain controller that enforces LDAP signing rejects a Kerberos bind that
	// negotiates no security layer, which is what a current Windows Server does by
	// default: "the server requires binds to turn on integrity checking if SSL\TLS
	// are not already active on the connection". So a security layer is negotiated
	// on plain LDAP, and not over LDAPS, where the TLS channel already protects the
	// connection and Active Directory refuses a SASL layer stacked on top of it.
	if cfg.Network.LDAP.UseKerberos && !cfg.Network.LDAP.UseLdaps {
		if cfg.Network.LDAP.UseSealing {
			ldapSession.SetGSSAPISealing()
		} else {
			ldapSession.SetGSSAPISigning()
		}
	}

	// Connect already names the server and the transport in its error, so it is
	// returned as-is rather than wrapped into the same sentence twice.
	connected, err := ldapSession.Connect()
	if err != nil {
		return nil, err
	}
	if !connected {
		return nil, fmt.Errorf("error connecting to LDAP server")
	}

	return ldapSession, nil
}

// hasSecret reports whether the credentials carry anything to authenticate with.
//
// A bind with no secret succeeds against a domain controller as an anonymous one, so
// the connection message has to say which of the two happened rather than claim an
// authentication that did not take place.
//
// Parameters:
//
//	creds (*credentials.Credentials): The credentials the session was built with.
//
// Returns:
//
//	True when a secret was supplied, false otherwise.
func hasSecret(creds *credentials.Credentials) bool {
	return creds.GetPassword() != "" ||
		creds.CanPassTheHash() ||
		creds.CanUseAESKey() ||
		creds.CanUseCCache() ||
		creds.CanUseKirbi() ||
		creds.CanUseKeytab()
}

// resolveSearchBases determines what to monitor: the search base the caller asked
// for, or every naming context the domain controller advertises.
//
// Parameters:
//
//	ldapSession (*ldap.Session): The connected LDAP session to query.
//	searchBase (string): The distinguished name to monitor, or empty for all naming contexts.
//
// Returns:
//
//	The distinguished names to monitor, or an error if the requested search base
//	does not exist or the naming contexts could not be read.
func resolveSearchBases(ldapSession *ldap.Session, searchBase string) ([]string, error) {
	searchBase = strings.TrimSpace(searchBase)

	if searchBase == "" {
		namingContexts, err := ldapSession.GetAllNamingContexts()
		if err != nil {
			return nil, fmt.Errorf("error fetching the naming contexts: %w", err)
		}
		return namingContexts, nil
	}

	// A search base that does not exist yields an empty snapshot and a run that
	// silently reports nothing forever, so it is rejected up front.
	exists, err := ldapSession.DistinguishedNameExists(searchBase)
	if err != nil {
		return nil, fmt.Errorf("error checking search base '%s': %w", searchBase, err)
	}
	if !exists {
		return nil, fmt.Errorf("search base '%s' does not exist", searchBase)
	}

	return []string{searchBase}, nil
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
