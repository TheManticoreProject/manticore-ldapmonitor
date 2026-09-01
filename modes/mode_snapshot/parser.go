// Package mode_snapshot reads every object of a domain once and writes them to a
// file, to be compared later with another reading.
package mode_snapshot

import (
	"fmt"

	"github.com/TheManticoreProject/Manticore/logger"
	"github.com/TheManticoreProject/goopts/parser"

	"github.com/TheManticoreProject/manticore-ldapmonitor/cli"
)

// SetupSubParser registers the snapshot mode and the argument groups it carries.
//
// It carries no reporting group: nothing is reported by a mode that writes a file,
// and what to leave out of a comparison is decided when the comparison happens.
//
// Parameters:
//
//	ap (*parser.ArgumentsParser): The top-level parser to register the mode on.
//	flags (*cli.Flags): The flag storage to bind to.
func SetupSubParser(ap *parser.ArgumentsParser, flags *cli.Flags) {
	subparser := ap.AddSubParser("snapshot", "Read every object of a domain once and write them to a file.")

	cli.RegisterConnectionGroups(subparser, flags)
	cli.RegisterScopeGroup(subparser, flags.SearchBase, flags.LDAPFilter)

	if group, err := subparser.NewArgumentGroup("Output"); err != nil {
		logger.Warn(fmt.Sprintf("Error creating ArgumentGroup: %s", err))
	} else {
		group.NewStringArgument(flags.OutputFile, "-o", "--outputfile", "", true, "File to write the reading to. It is gzipped when the name ends in .gz, which is worth doing on anything but a small scope.")
	}
}
