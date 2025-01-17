package node_starter

import (
	"errors"
	"fmt"
	"os/exec"
	"time"

	"code.cloudfoundry.org/lager"
	"github.com/cloudfoundry/mariadb_ctrl/cluster_health_checker"
	"github.com/cloudfoundry/mariadb_ctrl/config"
	"github.com/cloudfoundry/mariadb_ctrl/mariadb_helper"
	"github.com/cloudfoundry/mariadb_ctrl/os_helper"
)

type prestarter struct {
	mariaDBHelper        mariadb_helper.DBHelper
	osHelper             os_helper.OsHelper
	clusterHealthChecker cluster_health_checker.ClusterHealthChecker
	config               config.StartManager
	logger               lager.Logger
	mysqlCmd             *exec.Cmd
	finalState           string
}

func NewPreStarter(
	mariaDBHelper mariadb_helper.DBHelper,
	osHelper os_helper.OsHelper,
	config config.StartManager,
	logger lager.Logger,
	healthChecker cluster_health_checker.ClusterHealthChecker,
) Starter {
	return &prestarter{
		mariaDBHelper:        mariaDBHelper,
		osHelper:             osHelper,
		config:               config,
		logger:               logger,
		clusterHealthChecker: healthChecker,
		finalState:           "",
	}
}

func (s *prestarter) StartNodeFromState(state string) (string, error) {
	var err error
	var newNodeState string

	switch state {
	case SingleNode:
		newNodeState = SingleNode
	case NeedsBootstrap:
		if s.clusterHealthChecker.HealthyCluster() {
			err = s.startNodeAsJoiner()
			newNodeState = Clustered
		} else {
			newNodeState = NeedsBootstrap
		}
	case Clustered:
		err = s.joinCluster()
		newNodeState = Clustered
	default:
		err = fmt.Errorf("Unsupported state file contents: %s", state)
	}

	if err != nil {
		return "", err
	}

	if s.mysqlCmd != nil {
		dbch := s.waitForDatabaseToAcceptConnections()
		cmch := s.osHelper.WaitForCommand(s.mysqlCmd)
		select {
		case <-dbch:
			err = s.shutdownMysqld()
			if err != nil {
				return "", err
			}
		case err = <-cmch:
			return "", errors.New("mysqld stopped. Please check mysql.err.log")
		}
	}

	s.finalState = newNodeState

	return newNodeState, nil
}

func (s *prestarter) GetMysqlCmd() (*exec.Cmd, error) {
	if s.mysqlCmd != nil || (s.mysqlCmd == nil && s.finalState != Clustered) {
		return s.mysqlCmd, nil
	}
	return nil, errors.New("mysqld has not been started")
}

func (s *prestarter) startNodeAsJoiner() error {
	s.logger.Info("Joining an existing cluster")
	cmd, err := s.mariaDBHelper.StartMysqldInJoin()
	if err != nil {
		return err
	}

	s.mysqlCmd = cmd

	return nil
}

func (s *prestarter) joinCluster() (err error) {
	s.logger.Info("Joining a multi-node cluster")
	cmd, err := s.mariaDBHelper.StartMysqldInJoin()

	if err != nil {
		return err
	}

	s.mysqlCmd = cmd

	return nil
}

func (s *prestarter) waitForDatabaseToAcceptConnections() chan string {
	ch := make(chan string)
	s.logger.Info("Attempting to reach database.")
	numTries := 0
	go func() {
		for {
			if s.mariaDBHelper.IsDatabaseReachable() {
				s.logger.Info(fmt.Sprintf("Database became reachable after %d seconds", numTries*StartupPollingFrequencyInSeconds))
				ch <- "done"
				return
			}
			s.logger.Info("Database not reachable, retrying...")
			s.osHelper.Sleep(StartupPollingFrequencyInSeconds * time.Second)
			numTries++
		}
	}()
	return ch
}

func (s *prestarter) shutdownMysqld() error {
	s.logger.Info("Shutting down mysqld after prestart")
	return s.mariaDBHelper.StopMysqld()
}
