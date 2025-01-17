package start_manager

import (
	"fmt"
	"os/exec"
	"strings"

	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/mariadb_ctrl/cluster_health_checker"
	"github.com/cloudfoundry/mariadb_ctrl/config"
	"github.com/cloudfoundry/mariadb_ctrl/mariadb_helper"
	"github.com/cloudfoundry/mariadb_ctrl/os_helper"
	"github.com/cloudfoundry/mariadb_ctrl/start_manager/node_starter"
	"github.com/cloudfoundry/mariadb_ctrl/upgrader"
)

const (
	BootstrapCommand = "bootstrap"
	JoinCommand      = "start"
)

//go:generate counterfeiter . StartManager

type StartManager interface {
	Execute() error
	GetMysqlCmd() (*exec.Cmd, error)
	Shutdown() error
}

type startManager struct {
	osHelper      os_helper.OsHelper
	config        config.StartManager
	mariaDBHelper mariadb_helper.DBHelper
	upgrader      upgrader.Upgrader
	startCaller   node_starter.Starter
	logger        lager.Logger
	healthChecker cluster_health_checker.ClusterHealthChecker
	mysqlCmd      *exec.Cmd
}

func New(
	osHelper os_helper.OsHelper,
	config config.StartManager,
	mariaDBHelper mariadb_helper.DBHelper,
	upgrader upgrader.Upgrader,
	startCaller node_starter.Starter,
	logger lager.Logger,
	healthChecker cluster_health_checker.ClusterHealthChecker,
) StartManager {
	return &startManager{
		osHelper:      osHelper,
		config:        config,
		logger:        logger,
		mariaDBHelper: mariaDBHelper,
		upgrader:      upgrader,
		startCaller:   startCaller,
		healthChecker: healthChecker,
	}
}

func (m *startManager) Execute() error {
	var newNodeState string
	var err error

	if m.mariaDBHelper.IsProcessRunning() {
		m.logger.Info("mysqld process is already running, shutting down before continuing")
		err = m.Shutdown()
		if err != nil {
			m.logger.Error("Failed to shutdown mysql process", err)
			return err
		}
	}

	needsUpgrade, err := m.upgrader.NeedsUpgrade()
	if err != nil {
		m.logger.Info("Failed to determine upgrade status with error", lager.Data{"err": err.Error()})
		return err
	}
	if needsUpgrade {
		err = m.upgrader.Upgrade()
		if err != nil {
			m.logger.Info("Failed during upgrade", lager.Data{"err": err.Error()})
			return err
		}
	}

	m.logger.Info("Determining bootstrap procedure", lager.Data{
		"ClusterIps": m.config.ClusterIps,
		"MyIP":       m.config.MyIP,
	})

	currentState, err := m.getCurrentNodeState()
	if err != nil {
		return err
	}

	newNodeState, err = m.startCaller.StartNodeFromState(currentState)
	if err != nil {
		return err
	}

	err = m.writeStringToFile(newNodeState)
	if err != nil {
		return err
	}

	return nil
}

func (m *startManager) getCurrentNodeState() (string, error) {

	// Single-node deploy always requires bootstrapping of new cluster
	if len(m.config.ClusterIps) == 1 {
		return node_starter.SingleNode, nil
	}

	if m.firstTimeDeploy() {
		if m.config.MyIP == m.config.ClusterIps[0] {
			return node_starter.NeedsBootstrap, nil
		}

		return node_starter.Clustered, nil
	}

	// If we are not a first time deploy we must already have a state file
	state, err := m.readStateFromFile()
	if err != nil {
		m.logger.Info("state file could not be read", lager.Data{"err": err.Error()})
		return "", err
	}

	if state == node_starter.SingleNode && len(m.config.ClusterIps) > 1 {
		// Upgrading from a single-node cluster means we have to re-bootstrap
		return node_starter.NeedsBootstrap, nil
	}

	return state, nil
}

func (m *startManager) readStateFromFile() (string, error) {
	state, err := m.osHelper.ReadFile(m.config.StateFileLocation)
	if err != nil {
		return "", err
	}
	state = strings.TrimSpace(state)
	m.logger.Info(fmt.Sprintf("state file exists and contains: '%s'", state))
	return state, nil
}

func (m *startManager) firstTimeDeploy() bool {
	return !m.osHelper.FileExists(m.config.StateFileLocation)
}

func (m *startManager) GetMysqlCmd() (*exec.Cmd, error) {
	return m.startCaller.GetMysqlCmd()
}

func (m *startManager) Shutdown() error {
	m.logger.Info("Shutting down mysqld")
	return m.mariaDBHelper.StopMysqld()
}

func (m *startManager) writeStringToFile(contents string) error {
	m.logger.Info(fmt.Sprintf("updating file with contents: '%s'", contents))
	return m.osHelper.WriteStringToFile(m.config.StateFileLocation, contents)
}
