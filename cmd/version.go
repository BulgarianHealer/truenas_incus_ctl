package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

/*
0.1.0 initial version
0.1.1 added url, apikey and keyfile
0.1.2 added `share nfs` functionality
0.1.3 improved querying, removed --name and --id from `share nfs list`
0.1.4 added `--update-shares` to `dataset rename`
0.1.5 removed inspect command
0.1.6 `share nfs delete“ now supports <id|dataset|path>
0.1.7 `share nfs update now supports `--create`
0.1.8 most methods now return non-zero on error
0.2.0 renamed to `truenas_incus_ctl`, first published version
0.3.0 added bulk api calls, allowing for multiple datasets, snaps, shares to be edited in one go
0.3.1 fixed job waiting
0.3.2 added snapshot create --delete flag
0.4.0 added replication
0.4.1 dataset list -p fix
0.4.2 added additional repplication options
0.4.3 Increased timeout for asynchronous API calls
0.4.4 Snapshot lists are now sorted by dataset then txg
0.5.0 Add initial iSCSI support
0.5.1 Full support for iSCSI, added human-readable size parsing
0.5.2 Add a connection daemon, allowing for logins to be cached, more flexibility in handling jobs, etc
0.5.3 `share iscsi locate --activate/--deactivate`
0.6.0 added config command, changed definition of global flags
0.6.1 `share iscsi locate --create/--delete`
0.6.2 `snapshot rename` now calls zfs.snapshot.rename end-point
0.7.0 Add service commands, iscsi test, --daemon-socket to override path to the daemon's socket, add --portal and --initiator flags
0.7.1 Sends a sendtargets command before a plain discover. This seems to be required before verifying a portal, adds delete and deactivate support waiting for deactivation.
0.7.2 Deactivate synchronizes devices, and then optionally waits for deactivation t complete. Delete always waits. The daemon supports retry after POST failure and uses additional connections for concurrent commands
0.7.3 IPv6 fixes
0.7.4 Add :port support to --host
0.7.5 Add `share iscsi refresh` to refresh the iscsi bus
0.7.6 Fix macos/windows compilation issues
0.7.7 Accept integer volblocksize, ignore stderr from iscsiadm if return code is 0
0.7.7+fh1..fh3 NVMe-oF support over nvmet.*, transport selected per profile in config.json
0.7.7+fh4 Makes NVMe-oF usable as an Incus pool. The pool, the zvol and the connection all worked, yet no instance would launch: the NVMe path ignored the OUTPUT CONTRACT the driver reads. Incus parses lines, not exit codes.
0.7.7+fh4 From `locate` it expects `created\t…`, `activated\t…`, `located\t…` WITH the prefixes, even under --parsable (doIscsiActivate is called from locateIscsi with shouldPrintStatus=true). A bare path is read as something else and the driver reports "unable to activate" over a healthy device. Fixed in create, activate and locate.
0.7.7+fh4 `locate` with no flags now reports an already-attached volume at all. It was a pure flag dispatcher returning nothing, which produced "Unable to create, activate or locate TrueNAS volume: <vol>, " with an empty quote.
0.7.7+fh4 `test|list|refresh` are redirected too: `list` reported nothing, `refresh` rescanned the iSCSI bus so a grown volume never reached the guest, `test` required the iSCSI service on an all-NVMe appliance.
0.7.7+fh4 `test` tolerates a port with no subsystems — nvmet only listens once one is linked. Rollback deletes in reverse order, as nvmet silently refuses to drop a subsystem still linked to a port. `setup` starts the nvmet service instead of only complaining.
0.7.7+fh4 Adds `share nvme test|refresh|devices` and a marker-gated argv+output log (ArgvLogPath), because Incus never logs the commands it runs.
*/
const VERSION = "0.7.7"

// Build се подава при компилация:
//   -ldflags "-X truenas/truenas_incus_ctl/cmd.Build=fh1"
// Пакетният binary от incus-base няма стойност тук, нашият има — панелът
// различава двата само по низа на версията.
var Build string

func FullVersion() string {
	if Build != "" {
		return VERSION + "+" + Build
	}
	return VERSION
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version of this program",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(FullVersion())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
