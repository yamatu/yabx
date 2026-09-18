package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version  = "TempVersion" //use ldflags replace
	codename = "V2bX"
	intro    = "A V2board backend based on multi core"
)

var versionCommand = cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(_ *cobra.Command, _ []string) {
		showVersion()
	},
}

func init() {
	command.AddCommand(&versionCommand)
}

func showVersion() {
	fmt.Println(` 
  _/      _/    _/_/    _/        _/      _/   
 _/      _/  _/    _/  _/_/_/      _/  _/      
_/      _/      _/    _/    _/      _/         
 _/  _/      _/      _/    _/    _/  _/        
  _/      _/_/_/_/  _/_/_/    _/      _/        
                                                `)
	fmt.Printf("%s %s (%s) \n", codename, version, intro)
	// One line per compiled kernel, so `V2bX version` and the startup banner name
	// the cores this build carries. V2bX.sh greps these lines for its menu.
	for _, coreVersion := range installedCoreVersions() {
		fmt.Println(coreVersion)
	}
	//fmt.Printf("Supported cores: %s\n", strings.Join(vCore.RegisteredCore(), ", "))
	// Warning
	//fmt.Println(Warn("This version need V2board version >= 1.7.0."))
	//fmt.Println(Warn("The version have many changed for config, please check your config file"))
}
