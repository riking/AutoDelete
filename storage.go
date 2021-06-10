package autodelete

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
)

// Interface to the storage systems.
type Storage interface {
	ListChannels() ([]string, error)
	// Special errors:
	//  - os.IsNotExist() - no configuration for channel
	GetChannel(id string) (ManagedChannelMarshal, error)
	SaveChannel(conf ManagedChannelMarshal) error
	DeleteChannel(id string) error

	IsBanned(guildID string) (bool, error)
	AddBan(guildID string) error
}

/******************
 *  Disk Storage  *
 ******************/

// Stores channel configurations on disk as YAML files.
type DiskStorage struct {
}

const pathChannelConfDir = "./data"
const pathChannelConfig = "./data/%s.yml"
const pathBanList = "./data/bans.yml"

func (s *DiskStorage) ListChannels() ([]string, error) {
	files, err := ioutil.ReadDir(pathChannelConfDir)
	if err != nil {
		return nil, err
	}
	channelIDs := make([]string, 0, len(files))
	for _, v := range files {
		n := v.Name()
		if !strings.HasSuffix(n, ".yml") {
			continue
		}
		if strings.HasPrefix(n, "bans.yml") {
			continue
		}
		chID := strings.TrimSuffix(n, ".yml")
		channelIDs = append(channelIDs, chID)
	}
	return channelIDs, nil
}
func (s *DiskStorage) GetChannel(channelID string) (ManagedChannelMarshal, error) {
	var conf ManagedChannelMarshal

	fileName := fmt.Sprintf(pathChannelConfig, channelID)
	f, err := os.Open(fileName)
	if os.IsNotExist(err) {
		return conf, os.ErrNotExist
	} else if err != nil {
		return conf, err
	}
	by, err := ioutil.ReadAll(f)
	f.Close()
	if err != nil {
		return conf, err
	}
	err = yaml.Unmarshal(by, &conf)
	if err != nil {
		return conf, err
	}

	conf = internalMigrateConfig(conf)
	return conf, nil
}

func (s *DiskStorage) SaveChannel(conf ManagedChannelMarshal) error {
	conf = internalMigrateConfig(conf)
	by, err := yaml.Marshal(conf)
	if err != nil {
		panic(err)
	}
	fileName := fmt.Sprintf(pathChannelConfig, conf.ID)
	f, err := os.Create(fileName)
	if err != nil {
		return err
	}
	f.Write(by)
	err = f.Close()
	if err != nil {
		return err
	}
	return nil
}
func (s *DiskStorage) DeleteChannel(id string) error {
	fileName := fmt.Sprintf(pathChannelConfig, id)
	err := os.Remove(fileName)
	if err != nil {
		return err
	}
	return nil
}

func (s *DiskStorage) IsBanned(guildID string) (bool, error) {
	by, err := ioutil.ReadFile(pathBanList)
	if os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	var conf BansFile
	err = yaml.Unmarshal(by, &conf)
	if err != nil {
		return false, err
	}

	for _, v := range conf.Guilds {
		if v == guildID {
			return true, nil
		}
	}
	return false, nil
}

func (s *DiskStorage) AddBan(guildID string) error {
	return fmt.Errorf("unimplemented!")
}
