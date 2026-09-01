package mode_snapshot

import (
	"fmt"

	"github.com/TheManticoreProject/Manticore/logger"

	"github.com/TheManticoreProject/manticore-ldapmonitor/config"
	"github.com/TheManticoreProject/manticore-ldapmonitor/directory"
	"github.com/TheManticoreProject/manticore-ldapmonitor/utils"
)

// Run reads every object in scope once and writes them to a file.
//
// The file holds the scope the reading was taken with, so that a later diff can say
// when the two files it was handed do not cover the same ground.
//
// Parameters:
//
//	cfg (config.Config): The configuration of the run.
//
// Returns:
//
//	An error if connecting to the domain controller, reading from it, or writing the
//	file failed.
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
	utils.AnnounceSearchBases("Search bases", searchBases)

	snapshot, err := directory.TakeSnapshot(ldapSession, cfg.Scope, cfg.Debug)
	if err != nil {
		return err
	}
	logger.Print(fmt.Sprintf("[>] Objects read: \x1b[93m%d\x1b[0m.", len(snapshot)))

	stored := &directory.StoredSnapshot{
		Version:          cfg.Version,
		Domain:           cfg.Network.Domain,
		DomainController: cfg.Network.DomainController,
		Scope:            cfg.Scope,
	}
	stored.SetSnapshot(snapshot)

	if err := directory.WriteSnapshot(cfg.OutputFile, stored); err != nil {
		return err
	}

	logger.Print(fmt.Sprintf("[+] Reading written to \x1b[94m%s\x1b[0m.", directory.FormatText(cfg.OutputFile)))
	return nil
}
