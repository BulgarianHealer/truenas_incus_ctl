package cmd

import (
	"fmt"
	"strings"
	"time"
	"truenas/truenas_incus_ctl/core"

	"github.com/spf13/cobra"
)

// NVMe-oF споделяния през nvmet.* API-то на TrueNAS 26+.
//
// Командите отразяват iSCSI едно към едно — create, activate, deactivate, locate, delete —
// защото това е договорът, който Incus очаква.
//
// Incus 7.4 НЕ знае за NVMe: в целите му конфигурационни метаданни няма нито едно такова
// споменаване, а драйверът `truenas` е обвивка около този бинар (Incus докладва версията
// на драйвера като версията на инструмента). Затова изборът на транспорт живее в ПРОФИЛА
// на config.json, а Incus го избира по име през ключа `truenas.config`. При `transport=nvme`
// командите `share iscsi …` се пренасочват насам — Incus иска път до блоково устройство и
// го получава, без да знае какво има отдолу.
//
// Това е съзнателен компромис: командата се казва iscsi, а прави NVMe. Алтернативата беше
// кръпка в самия Incus, а при „винаги най-новата версия" тя значи прекърпване на всяко
// издание. Затова пренасочването е шумно на всяко място, където се случва.

var nvmeCmd = &cobra.Command{
	Use:   "nvme",
	Short: "Create, list or delete NVMe-oF subsystems that map to the given datasets",
}

var nvmeCreateCmd = &cobra.Command{
	Use:   "create <dataset>...",
	Short: "Create NVMe-oF subsystems and namespaces that map to the given datasets",
	Args:  cobra.MinimumNArgs(1),
}

var nvmeDeleteCmd = &cobra.Command{
	Use:   "delete <dataset>...",
	Short: "Delete the NVMe-oF subsystems that map to the given datasets",
	Args:  cobra.MinimumNArgs(1),
}

var nvmeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List NVMe-oF subsystems with their namespaces",
	Args:  cobra.ExactArgs(0),
}

var nvmeActivateCmd = &cobra.Command{
	Use:   "activate <dataset>...",
	Short: "Connect to the NVMe-oF subsystems that map to the given datasets",
	Args:  cobra.MinimumNArgs(1),
}

var nvmeDeactivateCmd = &cobra.Command{
	Use:   "deactivate <dataset>...",
	Short: "Disconnect from the NVMe-oF subsystems that map to the given datasets",
	Args:  cobra.MinimumNArgs(1),
}

var nvmeLocateCmd = &cobra.Command{
	Use:   "locate <dataset>...",
	Short: "Create and/or connect in one call, printing the device path",
	Args:  cobra.MinimumNArgs(1),
}

var nvmeSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Ensure the NVMe-oF service is started and a port is configured",
	Args:  cobra.ExactArgs(0),
}

var nvmeTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test an NVMe-oF port connection, and optionally set one up as well",
	Args:  cobra.ExactArgs(0),
}

var nvmeRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Rescan the NVMe-oF controllers to pick up any device changes",
	Args:  cobra.ExactArgs(0),
}

var nvmeDevicesCmd = &cobra.Command{
	Use:   "devices",
	Short: "List the local block devices of the connected NVMe-oF subsystems",
	Args:  cobra.ExactArgs(0),
}

func init() {
	nvmeCreateCmd.RunE = WrapCommandFunc(createNvme)
	nvmeDeleteCmd.RunE = WrapCommandFunc(deleteNvme)
	nvmeListCmd.RunE = WrapCommandFunc(listNvme)
	nvmeSetupCmd.RunE = WrapCommandFunc(setupNvme)
	nvmeActivateCmd.RunE = WrapCommandFunc(activateNvme)
	nvmeDeactivateCmd.RunE = WrapCommandFunc(deactivateNvme)
	nvmeLocateCmd.RunE = WrapCommandFunc(locateNvme)
	nvmeTestCmd.RunE = WrapCommandFunc(testNvme)
	nvmeRefreshCmd.RunE = WrapCommandFunc(refreshNvme)
	nvmeDevicesCmd.RunE = WrapCommandFunc(listNvmeDevices)

	nvmeTestCmd.Flags().Bool("setup", false, "Ensure the NVMe-oF service is started and a port is configured")
	nvmeSetupCmd.Flags().Bool("test", false, "Test the NVMe-oF port connection")

	for _, c := range []*cobra.Command{nvmeCreateCmd, nvmeDeleteCmd, nvmeListCmd, nvmeSetupCmd,
		nvmeActivateCmd, nvmeDeactivateCmd, nvmeLocateCmd, nvmeTestCmd} {
		c.Flags().StringP("target-prefix", "t", "", "label to prefix the created subsystem name")
		c.Flags().Bool("parsable", false, "Parsable (ie. minimal) output")
	}
	nvmeDeactivateCmd.Flags().Bool("wait", false, "Wait for the device to disappear")
	for _, c := range []*cobra.Command{nvmeLocateCmd} {
		c.Flags().Bool("create", false, "Create the subsystem if missing")
		c.Flags().Bool("activate", false, "Connect after creating")
		c.Flags().Bool("deactivate", false, "Disconnect instead")
		c.Flags().Bool("delete", false, "Delete instead")
		c.Flags().Bool("wait", false, "Wait for the device to disappear on deactivate")
	}
	for _, c := range []*cobra.Command{nvmeCreateCmd, nvmeDeleteCmd, nvmeActivateCmd,
		nvmeDeactivateCmd, nvmeLocateCmd, nvmeTestCmd} {
		c.Flags().String("port", "", "NVMe-oF port id or [ip]:[port]. Defaults to the only configured port")
		c.Flags().String("host-nqn", "", "Initiator NQN allowed to access the subsystem. Empty means any host")
	}

	nvmeCmd.AddCommand(nvmeCreateCmd)
	nvmeCmd.AddCommand(nvmeDeleteCmd)
	nvmeCmd.AddCommand(nvmeListCmd)
	nvmeCmd.AddCommand(nvmeSetupCmd)
	nvmeCmd.AddCommand(nvmeActivateCmd)
	nvmeCmd.AddCommand(nvmeDeactivateCmd)
	nvmeCmd.AddCommand(nvmeLocateCmd)
	nvmeCmd.AddCommand(nvmeTestCmd)
	nvmeCmd.AddCommand(nvmeRefreshCmd)
	nvmeCmd.AddCommand(nvmeDevicesCmd)
	AddNvmetCrudCommands(nvmeCmd)

	shareCmd.AddCommand(nvmeCmd)
}

// nvmeSubsysNameFromVolume прави името на subsystem-а от пътя на тома.
//
// TrueNAS сглобява пълния subnqn от `basenqn` + това име, затова тук се подава само
// името — така NQN-ът остава последователен с останалите споделяния на уреда, вместо
// да съжителстват два формата.
func nvmeSubsysNameFromVolume(prefix, vol string) string {
	name := strings.ToLower(vol)
	name = strings.NewReplacer("/", "-", "@", "-", ":", "-", "_", "-", ".", "-").Replace(name)
	name = strings.Trim(name, "-")
	if prefix != "" {
		name = prefix + "-" + name
	}
	return name
}

func nvmeDevicePath(vol string) string {
	if strings.HasPrefix(vol, "zvol/") || strings.HasPrefix(vol, "/") {
		return vol
	}
	return "zvol/" + vol
}

func createNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	options, _ := GetCobraFlags(cmd, false, nil)

	prefix := strings.TrimSpace(options.allFlags["target_prefix"])
	hostNqn := strings.TrimSpace(options.allFlags["host_nqn"])
	isParsable := core.IsStringTrue(options.allFlags, "parsable")

	// Всяко създадено нещо влиза тук и се отменя при провал — включително двете join
	// таблици. При iSCSI targetextent.create НЕ се записва (iscsi.go:350-355) и остава
	// сирак, ако следващата стъпка гръмне. Отмяната е в обратен ред — защо, виж
	// undoNvmeCreateList.
	changes := make([]typeApiCallRecord, 0)
	defer func() { undoNvmeCreateList(api, &changes) }()

	portId, err := LookupNvmePort(api, nvmePortSpec(options))
	if err != nil {
		return err
	}

	hostId := -1
	if hostNqn != "" {
		if hostId, err = LookupNvmeHostOrCreate(api, hostNqn); err != nil {
			return err
		}
		if hostId == -1 {
			return fmt.Errorf("could not find or create an NVMe-oF host for %q", hostNqn)
		}
	}

	created := make([]string, 0, len(args))

	for _, vol := range args {
		name := nvmeSubsysNameFromVolume(prefix, vol)
		devPath := nvmeDevicePath(vol)

		// 1. subsystem — по име. Без ранния изход на iscsi.go:194-199: дори когато
		// subsystem-ът вече е налице, връзките и namespace-ът се сверяват, за да се
		// самолекува полусъздадено състояние.
		subsysId, err := lookupNvmeIdByFilter(api, "nvmet.subsys",
			[]interface{}{[]interface{}{"name", "=", name}})
		if err != nil {
			return err
		}
		if subsysId == -1 {
			obj := map[string]interface{}{
				"name":           name,
				"allow_any_host": hostNqn == "",
			}
			if subsysId, err = createNvmeObject(api, "nvmet.subsys", obj); err != nil {
				return err
			}
			if subsysId == -1 {
				return fmt.Errorf("could not create an NVMe-oF subsystem named %q", name)
			}
			changes = append(changes, typeApiCallRecord{
				endpoint:   "nvmet.subsys.create",
				resultList: []interface{}{map[string]interface{}{"id": subsysId}},
			})
		}

		// 2. порт ↔ subsystem
		linkId, isNew, err := EnsureNvmeLink(api, "nvmet.port_subsys",
			map[string]interface{}{"port_id": portId, "subsys_id": subsysId})
		if err != nil {
			return err
		}
		if isNew {
			changes = append(changes, typeApiCallRecord{
				endpoint:   "nvmet.port_subsys.create",
				resultList: []interface{}{map[string]interface{}{"id": linkId}},
			})
		}

		// 3. host ↔ subsystem, само при зададен hostnqn
		if hostId != -1 {
			linkId, isNew, err = EnsureNvmeLink(api, "nvmet.host_subsys",
				map[string]interface{}{"host_id": hostId, "subsys_id": subsysId})
			if err != nil {
				return err
			}
			if isNew {
				changes = append(changes, typeApiCallRecord{
					endpoint:   "nvmet.host_subsys.create",
					resultList: []interface{}{map[string]interface{}{"id": linkId}},
				})
			}
		}

		// 4. namespace — филтрира се по subsystem И път, защото един и същ zvol може да
		// е изнесен в няколко subsystem-а.
		//
		// Полето е `subsys.id`, НЕ `subsys_id`. Второто се приема при СЪЗДАВАНЕ, но при
		// заявка връща нула записа, без да се оплаче — проверката за съществуващ namespace
		// винаги казваше „няма" и всяко повторно пускане опитваше дубликат. Отговорът
		// разгръща връзката в обект (`"subsys": {...}`), затова и филтърът е по пътя в него.
		nsId, err := lookupNvmeIdByFilter(api, "nvmet.namespace", []interface{}{
			[]interface{}{"subsys.id", "=", subsysId},
			[]interface{}{"device_path", "=", devPath},
		})
		if err != nil {
			return err
		}
		if nsId == -1 {
			obj := map[string]interface{}{
				"device_type": "ZVOL",
				"device_path": devPath,
				"subsys_id":   subsysId,
			}
			if nsId, err = createNvmeObject(api, "nvmet.namespace", obj); err != nil {
				return err
			}
			if nsId == -1 {
				return fmt.Errorf("could not create an NVMe-oF namespace for %q", devPath)
			}
			changes = append(changes, typeApiCallRecord{
				endpoint:   "nvmet.namespace.create",
				resultList: []interface{}{map[string]interface{}{"id": nsId}},
			})
		}

		created = append(created, name)
	}

	// COMMIT — оттук нататък нищо не се отменя.
	changes = nil

	// Същото условие като в createIscsi (iscsi.go:361) и по същата причина: когато
	// командата е извикана ОТ `locate`, изходът се чете от Incus като път до устройство.
	// Голото име на subsystem-а там минава за такъв път и драйверът тръгва по него,
	// вместо по истинския `/dev/nvmeXnY` на следващия ред. Префиксът `created\t` го
	// отличава. Точно това чупеше създаването на машина: командата е
	// `locate --create --parsable`, БЕЗ `--activate`.
	printBare := isParsable && !strings.HasPrefix(cmd.Use, "locate")
	for _, name := range created {
		if printBare {
			fmt.Println(name)
		} else {
			fmt.Printf("created\t%s\n", name)
		}
	}
	return nil
}

// undoNvmeCreateList отменя създаденото при провал — в ОБРАТЕН ред.
//
// undoIscsiCreateList обхожда напред и за iSCSI това минава. При nvmet не минава:
// `nvmet.subsys.delete` отказва, докато subsystem-ът още е закачен за порт — и го прави
// МЪЛЧАЛИВО, връщайки успех. Резултатът е subsystem-сирак без namespace и без връзка,
// който после блокира повторното създаване със същото име.
//
// Затова редът е строго обратният на създаването: namespace → host_subsys →
// port_subsys → subsys. Същият ред, който deleteNvme спазва.
func undoNvmeCreateList(api core.Session, changes *[]typeApiCallRecord) {
	DebugString("undoNvmeCreateList")
	for i := len(*changes) - 1; i >= 0; i-- {
		call := (*changes)[i]
		DebugString(call.endpoint)
		if !strings.HasSuffix(call.endpoint, ".create") {
			continue
		}
		idList := make([]interface{}, 0, len(call.resultList))
		for _, r := range call.resultList {
			idList = append(idList, []interface{}{core.GetIdFromObject(r)})
		}
		if len(idList) == 0 {
			continue
		}
		MaybeBulkApiCallArray(
			api,
			call.endpoint[:len(call.endpoint)-7]+".delete",
			defaultCallTimeout,
			idList,
			false,
		)
	}
}

func deleteNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	options, _ := GetCobraFlags(cmd, false, nil)

	prefix := strings.TrimSpace(options.allFlags["target_prefix"])
	isParsable := core.IsStringTrue(options.allFlags, "parsable")

	for _, vol := range args {
		name := nvmeSubsysNameFromVolume(prefix, vol)

		subsysId, err := lookupNvmeIdByFilter(api, "nvmet.subsys",
			[]interface{}{[]interface{}{"name", "=", name}})
		if err != nil {
			return err
		}
		if subsysId == -1 {
			fmt.Printf("not-found\t%s\n", name)
			continue
		}

		// Редът има значение: namespace-ите и връзките държат subsystem-а зает.
		// `subsys.id` при namespace — вж. бележката в createNvme.
		if err = deleteNvmeChildren(api, "nvmet.namespace", "subsys.id", subsysId); err != nil {
			return err
		}
		if err = deleteNvmeChildren(api, "nvmet.host_subsys", "subsys_id", subsysId); err != nil {
			return err
		}
		if err = deleteNvmeChildren(api, "nvmet.port_subsys", "subsys_id", subsysId); err != nil {
			return err
		}

		if _, err = core.ApiCall(api, "nvmet.subsys.delete", defaultCallTimeout,
			[]interface{}{subsysId}); err != nil {
			return err
		}

		if isParsable {
			fmt.Println(name)
		} else {
			fmt.Printf("deleted\t%s\n", name)
		}
	}
	return nil
}

// deleteNvmeChildren трие всички записи в endpoint, чието поле `key` сочи към id.
//
// Филтрира се на сървъра — вж. queryNvmeIdsByFilter.
func deleteNvmeChildren(api core.Session, endpoint, key string, id int) error {
	ids, err := queryNvmeIdsByFilter(api, endpoint,
		[]interface{}{[]interface{}{key, "=", id}})
	if err != nil {
		return err
	}

	for _, rowId := range ids {
		if _, err := core.ApiCall(api, endpoint+".delete", defaultCallTimeout,
			[]interface{}{rowId}); err != nil {
			return err
		}
	}
	return nil
}

func listNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true

	subsys, err := QueryApi(api, "nvmet.subsys", nil, nil, nil, typeQueryParams{})
	if err != nil {
		return err
	}

	// Отговорът разгръща връзката в обект (`"subsys": {...}`), а insertProperties
	// (util_common.go:424) свежда вложен обект до nil, освен ако valueOrder не каже кой
	// ключ да извади. С "id" връзката става точно идентификатора си.
	nsParams := typeQueryParams{valueOrder: []string{"id"}}
	namespaces, err := QueryApi(api, "nvmet.namespace", nil, nil, nil, nsParams)
	if err != nil {
		return err
	}

	nsBySubsys := make(map[string][]string)
	for _, ns := range GetListFromQueryResponse(&namespaces) {
		key := fmt.Sprint(ns["subsys"])
		nsBySubsys[key] = append(nsBySubsys[key], fmt.Sprint(ns["device_path"]))
	}

	for _, s := range GetListFromQueryResponse(&subsys) {
		paths := nsBySubsys[fmt.Sprint(s["id"])]
		fmt.Printf("%v\t%v\t%s\n", s["name"], s["subnqn"], strings.Join(paths, ","))
	}
	return nil
}

// activateNvme свързва нода към subsystem-а и връща пътя до блоковото устройство.
//
// Това е половината, заради която Incus изобщо може да ползва NVMe: неговият `truenas`
// драйвър не знае за NVMe, но и не му трябва — той пуска тази команда и чака път до
// устройство. Какъв е транспортът отдолу е наша работа.
func activateNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	if err := requireRootForNvme("activate"); err != nil {
		return err
	}
	options, _ := GetCobraFlags(cmd, false, nil)
	prefix := strings.TrimSpace(options.allFlags["target_prefix"])
	isParsable := core.IsStringTrue(options.allFlags, "parsable")

	portId, err := LookupNvmePort(api, nvmePortSpec(options))
	if err != nil {
		return err
	}
	addr, port, err := LookupNvmePortAddress(api, portId)
	if err != nil {
		return err
	}

	for _, vol := range args {
		name := nvmeSubsysNameFromVolume(prefix, vol)
		subNqn, err := LookupNvmeSubNqn(api, name)
		if err != nil {
			return err
		}
		if subNqn == "" {
			fmt.Printf("not-found\t%s\n", name)
			continue
		}

		if err := RunNvmeConnect(addr, port, subNqn); err != nil {
			return err
		}

		dev := WaitForNvmeDevice(subNqn, 30*time.Second)
		if dev == "" {
			fmt.Printf("timed-out\t%s\n", subNqn)
			continue
		}

		// Гол път само при пряко извикване. От `locate` Incus чака `activated\t<път>`
		// — така го прави и doIscsiActivate (iscsi.go:820), което се вика оттам с
		// shouldPrintStatus=true. Голият ред там се чете като нещо друго и драйверът
		// казва „unable to activate", макар устройството да е закачено.
		if isParsable && !strings.HasPrefix(cmd.Use, "locate") {
			fmt.Println(dev)
		} else {
			fmt.Printf("activated\t%s\n", dev)
		}
	}
	return nil
}

func deactivateNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	if err := requireRootForNvme("deactivate"); err != nil {
		return err
	}
	options, _ := GetCobraFlags(cmd, false, nil)
	prefix := strings.TrimSpace(options.allFlags["target_prefix"])
	shouldWait := core.IsStringTrue(options.allFlags, "wait")

	for _, vol := range args {
		name := nvmeSubsysNameFromVolume(prefix, vol)
		subNqn, err := LookupNvmeSubNqn(api, name)
		if err != nil {
			return err
		}
		if subNqn == "" {
			fmt.Printf("not-found\t%s\n", name)
			continue
		}

		if err := RunNvmeDisconnect(subNqn); err != nil {
			return err
		}
		if shouldWait && !WaitForNvmeDeviceGone(subNqn, 30*time.Second) {
			fmt.Printf("timed-out\t%s\n", subNqn)
			continue
		}
		fmt.Printf("deactivated\t%s\n", subNqn)
	}
	return nil
}

// locateNvme е „всичко наведнъж" — Incus ползва точно нея при закачане на том.
//
// Критично: БЕЗ флагове тя пак трябва да намери и отпечата вече закачения том. Дотук
// беше само диспечер по флаговете и при гол `locate` връщаше нула редове. Incus казваше
// „Unable to create, activate or locate TrueNAS volume: <том>, " — с празно място след
// запетаята, защото инструментът не беше извел нищо и нямаше какво да се цитира.
//
// locateIscsi винаги отпечатва намереното (`located\t<път>`), независимо от флаговете.
// Тук е същото, с една разлика: при iSCSI пътят се чете от /dev/disk/by-path, а тук от
// sysfs по NQN.
func locateNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	options, _ := GetCobraFlags(cmd, false, nil)

	prefix := strings.TrimSpace(options.allFlags["target_prefix"])
	shouldCreate := core.IsStringTrue(options.allFlags, "create")
	shouldDelete := core.IsStringTrue(options.allFlags, "delete")
	shouldDeactivate := core.IsStringTrue(options.allFlags, "deactivate") || shouldDelete
	shouldActivate := core.IsStringTrue(options.allFlags, "activate") || shouldCreate

	// Развалящите операции са изключващи и не отпечатват пътища.
	if shouldDelete {
		return deleteNvme(cmd, api, args)
	}
	if shouldDeactivate {
		return deactivateNvme(cmd, api, args)
	}

	if shouldCreate {
		if err := createNvme(cmd, api, args); err != nil {
			return err
		}
	}

	// Какво вече е закачено на този нод — отпечатва се веднага. Останалото чака
	// activate, ако е поискан.
	remaining := make([]string, 0, len(args))
	for _, vol := range args {
		subNqn, err := LookupNvmeSubNqn(api, nvmeSubsysNameFromVolume(prefix, vol))
		if err != nil {
			return err
		}
		dev := ""
		if subNqn != "" {
			dev = FindNvmeDeviceBySubNqn(subNqn)
		}
		if dev == "" {
			remaining = append(remaining, vol)
			continue
		}
		// locateIscsi печата „located\t<път>" безусловно (iscsi.go:650) — и при
		// --parsable. Форматът е договорът с драйвера, не козметика.
		fmt.Printf("located\t%s\n", dev)
	}

	if len(remaining) == 0 {
		return nil
	}
	if shouldActivate {
		return activateNvme(cmd, api, remaining)
	}
	for _, vol := range remaining {
		fmt.Printf("not-found\t%s\n", vol)
	}
	return nil
}

// nvmePortSpec приема и двете имена на флага.
//
// Собствените nvme команди го наричат `--port`, но когато Incus вика командата през
// `share iscsi`, там флагът е `--portal`. Една функция обслужва двете повърхности,
// вместо да се дублира логиката.
func nvmePortSpec(options FlagMap) string {
	if v := strings.TrimSpace(options.allFlags["port"]); v != "" {
		return v
	}

	// `--portal` по подразбиране е ":" при iSCSI — „адресът на хоста, портът по
	// подразбиране". За NVMe това не значи нищо и подадено както е дава
	// „no NVMe-oF port matches", вместо да се вземе единственият конфигуриран порт.
	portal := strings.TrimSpace(options.allFlags["portal"])
	if strings.Trim(portal, ":[] ") == "" {
		return ""
	}
	return portal
}

func setupNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	options, _ := GetCobraFlags(cmd, false, nil)

	if err := setupNvmeImpl(api, options); err != nil {
		return err
	}
	if core.IsStringTrue(options.allFlags, "test") {
		return testNvmeImpl(api, options, true)
	}
	return nil
}

// setupNvmeImpl пуска услугата, ако не върви, и намира порта.
//
// Огледало на setupIscsiImpl, включително пускането на услугата. Дотук setupNvme само
// се оплакваше, че `nvmet` е спряна — а iSCSI пътят я пуска сам. Разликата значеше, че
// `share iscsi setup` върши различна работа според транспорта.
//
// Порт НЕ се създава: nvmet.port иска адрес за слушане, който инструментът няма откъде
// да знае. Липсва ли порт, LookupNvmePort казва това с думи.
func setupNvmeImpl(api core.Session, options FlagMap) error {
	isMinimal := core.IsStringTrue(options.allFlags, "parsable")

	msg, err := CheckRemoteNvmetServiceIsRunning(api)
	if err != nil {
		return err
	}
	if msg != "" {
		if err := changeServiceStateImpl(api, "start", nil, true, false, []string{"nvmet"}); err != nil {
			return err
		}
		if !isMinimal {
			fmt.Println("Started and enabled nvmet service")
		}
	}

	portId, err := LookupNvmePort(api, nvmePortSpec(options))
	if err != nil {
		return err
	}
	if !isMinimal {
		fmt.Println("Port ID:", portId)
	}

	return nil
}

// testNvme доказва, че порталът отговаря — NVMe съответствието на `share iscsi test`.
//
// Incus вика точно тази проверка при създаване на пул. Без пренасочването тя оставаше на
// iSCSI и изискваше iSCSI услугата да е пусната на уреда, макар транспортът да е NVMe.
func testNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	options, _ := GetCobraFlags(cmd, false, nil)

	checkedServiceState := false
	if core.IsStringTrue(options.allFlags, "setup") {
		if err := setupNvmeImpl(api, options); err != nil {
			return err
		}
		checkedServiceState = true
	}
	return testNvmeImpl(api, options, checkedServiceState)
}

func testNvmeImpl(api core.Session, options FlagMap, checkedServiceState bool) error {
	if !checkedServiceState {
		msg, err := CheckRemoteNvmetServiceIsRunning(api)
		if err != nil {
			return err
		}
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}

	portId, err := LookupNvmePort(api, nvmePortSpec(options))
	if err != nil {
		return err
	}
	addr, port, err := LookupNvmePortAddress(api, portId)
	if err != nil {
		return err
	}

	isMinimal := core.IsStringTrue(options.allFlags, "parsable")

	// Празен порт НЕ е повреда. `nvmet` вдига слушателя чак когато за порта се закачи
	// първи subsystem — дотогава 4420 отказва връзка. Точно това е състоянието при
	// създаване на пул върху уред без споделяния, тоест случаят, в който Incus вика
	// тази проверка. Провал тук би отказал напълно изправен уред.
	//
	// Затова discovery се изисква само когато има какво да се открие.
	linked, err := CountNvmePortSubsys(api, portId)
	if err != nil {
		return err
	}
	if linked == 0 {
		if !isMinimal {
			fmt.Printf("NVMe-oF port %d at %s:%d is configured but has no subsystems yet\n", portId, addr, port)
		}
		return nil
	}

	discoveryOutput, err := RunNvmeDiscover(addr, port)
	if err != nil {
		return err
	}

	if !isMinimal {
		fmt.Println(discoveryOutput)
	}
	return nil
}

// listNvmeDevices изброява ЛОКАЛНО закачените устройства — не отдалечените subsystem-и.
//
// Двата списъка са различни неща и затова са две команди: `share nvme list` показва
// какво предлага уредът, а тази — какво е закачено на този нод. `share iscsi list`
// прави второто, затова пренасочването сочи насам.
func listNvmeDevices(cmd *cobra.Command, api core.Session, args []string) error {
	IterateConnectedNvmeSubsystems(func(subNqn string, devPaths []string) {
		for _, dev := range devPaths {
			fmt.Println(dev)
		}
	})
	return nil
}

// refreshNvme пресканира контролерите, за да се види порасналият том.
//
// Съответствието на `iscsiadm -m node -R`. Липсата на това пренасочване значеше, че при
// NVMe увеличаването на зает диск се пробва по iSCSI и не достига до госта.
func refreshNvme(cmd *cobra.Command, api core.Session, args []string) error {
	cmd.SilenceUsage = true
	if err := requireRootForNvme("refresh"); err != nil {
		return err
	}
	return RunNvmeRescan()
}

// isNvmeTransport казва дали профилът е конфигуриран за NVMe-oF вместо iSCSI.
//
// Проверява се в НАЧАЛОТО на всяка iSCSI команда, която Incus вика. Дотам конфигурацията
// вече е прочетена (InitializeApiClient върви в WrapCommandFunc преди самата функция).
func isNvmeTransport() bool {
	return g_transport == "nvme"
}

// dispatchNvme пренасочва iSCSI команда към NVMe и го КАЗВА в дебъг дневника.
//
// Мълчаливото пренасочване е рецепта за изгубен половин ден: човек чете `share iscsi
// activate` в дневника на Incus, търси iSCSI сесия и не намира нищо.
func dispatchNvme(verb string, fn func(*cobra.Command, core.Session, []string) error,
	cmd *cobra.Command, api core.Session, args []string) error {
	DebugString("transport=nvme: \"share iscsi " + verb + "\" is being served over NVMe-oF")
	return fn(cmd, api, args)
}
