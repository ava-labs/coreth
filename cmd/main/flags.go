package main

import (
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	ConfigFilePathKey = "config-file"
	RootKey           = "root"
	HeightKey         = "height"
	StartKey          = "start"
	EndKey            = "end"
	IPPortKey         = "ip"
)

func BuildViper(fs *pflag.FlagSet, args []string) (*viper.Viper, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.SetEnvPrefix("")
	if err := v.BindPFlags(fs); err != nil {
		return nil, err
	}

	if v.IsSet(ConfigFilePathKey) {
		v.SetConfigFile(v.GetString(ConfigFilePathKey))
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// BuildFlagSet returns a complete set of flags for simulator
func BuildFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("simulator", pflag.ContinueOnError)
	addSimulatorFlags(fs)
	return fs
}

func addSimulatorFlags(fs *pflag.FlagSet) {
	fs.String(ConfigFilePathKey, "", "Config file")
	fs.String(RootKey, "0x048821c4aea120d3151b42175752bf8a4dfb92f654779bb65e845cf63d4d71c8", "Specify the atomic trie root") // Default to recent root from logs
	fs.String(HeightKey, "38338560", "Specify the height of the atomic trie root")
	fs.Uint64(StartKey, 0, "Specify the start of the range")
	fs.Uint64(EndKey, 10_000, "Specify the end of the range to fetch")
	fs.String(IPPortKey, "127.0.0.1:9651", "Specify the IP Port pair to attempt to query")
}
