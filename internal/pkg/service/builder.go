package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/task"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/inbound"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/proxy"

	api "github.com/GoAsyncFunc/uniproxy/pkg"
)

type Config struct {
	NodeID   int
	NodeType string

	FetchUsersInterval     time.Duration
	ReportTrafficsInterval time.Duration
	HeartbeatInterval      time.Duration
	Cert                   *CertConfig
	ListenAddr             string
}

type Builder struct {
	instance                      *core.Instance
	config                        *Config
	nodeInfo                      *api.NodeInfo // Changed to specific type or keep generic? Using uniproxy NodeInfo
	inboundTag                    string
	userList                      []api.UserInfo
	userListMu                    sync.RWMutex
	apiClient                     *api.Client
	fetchUsersMonitorPeriodic     *task.Periodic
	reportTrafficsMonitorPeriodic *task.Periodic
	heartbeatMonitorPeriodic      *task.Periodic
	ctx                           context.Context
	cancel                        context.CancelFunc
}

func New(inboundTag string, instance *core.Instance, config *Config, nodeInfo *api.NodeInfo,
	apiClient *api.Client,
) *Builder {
	ctx, cancel := context.WithCancel(context.Background())
	return &Builder{
		inboundTag: inboundTag,
		instance:   instance,
		config:     config,
		nodeInfo:   nodeInfo,
		apiClient:  apiClient,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (b *Builder) Start() error {
	// Initial user fetch
	userList, err := b.apiClient.GetUserList(b.ctx)
	if err != nil {
		return err
	}
	err = b.addNewUser(userList)
	if err != nil {
		return err
	}
	b.userList = userList

	b.fetchUsersMonitorPeriodic = &task.Periodic{
		Interval: b.config.FetchUsersInterval,
		Execute:  b.fetchUsersMonitor,
	}
	b.reportTrafficsMonitorPeriodic = &task.Periodic{
		Interval: b.config.ReportTrafficsInterval,
		Execute:  b.reportTrafficsMonitor,
	}

	log.Infoln("Start monitoring for user acquisition")
	if err := b.fetchUsersMonitorPeriodic.Start(); err != nil {
		return fmt.Errorf("fetch users monitor periodic start error: %s", err)
	}

	log.Infoln("Start traffic reporting monitoring")
	if err := b.reportTrafficsMonitorPeriodic.Start(); err != nil {
		return fmt.Errorf("traffic monitor periodic start error: %s", err)
	}

	if b.config.HeartbeatInterval > 0 {
		b.heartbeatMonitorPeriodic = &task.Periodic{
			Interval: b.config.HeartbeatInterval,
			Execute:  b.heartbeatMonitor,
		}
		log.Infoln("Start heartbeat monitoring")
		if err := b.heartbeatMonitorPeriodic.Start(); err != nil {
			return fmt.Errorf("heartbeat monitor periodic start error: %s", err)
		}
	}
	return nil
}

func (b *Builder) Close() error {
	b.cancel()
	if b.fetchUsersMonitorPeriodic != nil {
		b.fetchUsersMonitorPeriodic.Close()
	}
	if b.reportTrafficsMonitorPeriodic != nil {
		b.reportTrafficsMonitorPeriodic.Close()
	}
	if b.heartbeatMonitorPeriodic != nil {
		b.heartbeatMonitorPeriodic.Close()
	}
	return nil
}

func (b *Builder) fetchUsersMonitor() error {
	newUserList, err := b.apiClient.GetUserList(b.ctx)
	if err != nil {
		log.Errorln(err)
		return nil
	}

	b.userListMu.RLock()
	currentUsers := b.userList
	b.userListMu.RUnlock()

	deleted, added := b.compareUserList(newUserList, currentUsers)
	if len(deleted) > 0 {
		deletedEmail := make([]string, len(deleted))
		for i, u := range deleted {
			deletedEmail[i] = buildUserEmail(b.inboundTag, u.Id, u.Uuid)
		}
		if err := b.removeUsers(deletedEmail, b.inboundTag); err != nil {
			log.Errorln(err)
			return nil
		}
	}
	if len(added) > 0 {
		if err := b.addNewUser(added); err != nil {
			log.Errorln(err)
			return nil
		}
	}
	if len(deleted) > 0 || len(added) > 0 {
		log.Infof("%d user deleted, %d user added", len(deleted), len(added))
	}
	b.userListMu.Lock()
	b.userList = newUserList
	b.userListMu.Unlock()
	return nil
}

func (b *Builder) reportTrafficsMonitor() error {
	b.userListMu.RLock()
	users := b.userList
	b.userListMu.RUnlock()

	userTraffic := make([]api.UserTraffic, 0)
	for _, user := range users {
		email := buildUserEmail(b.inboundTag, user.Id, user.Uuid)
		up, down, _ := b.getTraffic(email) // Count not used in uniproxy v1? Check model.
		if up > 0 || down > 0 {
			userTraffic = append(userTraffic, api.UserTraffic{
				UID:      user.Id,
				Upload:   int64(up),
				Download: int64(down),
			})
		}
	}
	if len(userTraffic) > 0 {
		log.Infof("%d user traffic needs to be reported", len(userTraffic))
		err := b.apiClient.ReportUserTraffic(b.ctx, userTraffic)
		if err != nil {
			log.Errorln("server error when submitting traffic", err)
			return nil
		}
	}
	return nil
}

func (b *Builder) heartbeatMonitor() error {
	// uniproxy has ReportNodeOnlineUsers? Or maybe just ReportNodeStatus?
	// Checking client.go... ReportNodeOnlineUsers(ctx, data)
	// If heartbeat implies just "I am alive" without users, maybe send empty?
	// Or maybe there is no heartbeat in uniproxy for simple alive check?
	// README said "Health Checks: Report node online status."
	// client.go has ReportNodeOnlineUsers.
	// We can report online users here if we track them, or just empty?
	// If we don't track online IP, maybe just empty map to signify heartbeat?
	data := make(map[int][]string)
	err := b.apiClient.ReportNodeOnlineUsers(b.ctx, data)
	if err != nil {
		log.Errorln("server error when sending heartbeat", err)
	}
	return nil
}

func (b *Builder) compareUserList(newUsers, oldUsers []api.UserInfo) (deleted, added []api.UserInfo) {
	oldUserMap := make(map[int]bool)
	for _, user := range oldUsers {
		oldUserMap[user.Id] = true
	}

	newUserMap := make(map[int]bool)
	for _, newUser := range newUsers {
		newUserMap[newUser.Id] = true
		if !oldUserMap[newUser.Id] {
			added = append(added, newUser)
		}
	}

	for _, oldUser := range oldUsers {
		if !newUserMap[oldUser.Id] {
			deleted = append(deleted, oldUser)
		}
	}
	return deleted, added
}

func (b *Builder) getTraffic(email string) (up int64, down int64, count int64) {
	var builder strings.Builder
	builder.Grow(64)
	builder.WriteString("user>>>")
	builder.WriteString(email)
	builder.WriteString(">>>traffic>>>uplink")
	upName := builder.String()

	builder.Reset()
	builder.Grow(64)
	builder.WriteString("user>>>")
	builder.WriteString(email)
	builder.WriteString(">>>traffic>>>downlink")
	downName := builder.String()

	statsManager := b.instance.GetFeature(stats.ManagerType()).(stats.Manager)
	upCounter := statsManager.GetCounter(upName)
	downCounter := statsManager.GetCounter(downName)

	if upCounter != nil {
		up = upCounter.Value()
		if up > 0 {
			upCounter.Set(0)
		}
	}
	if downCounter != nil {
		down = downCounter.Value()
		if down > 0 {
			downCounter.Set(0)
		}
	}
	return up, down, 0 // Count support might need similar logic if added
}

func (b *Builder) addNewUser(userInfo []api.UserInfo) error {
	users := buildUser(b.inboundTag, userInfo)
	if len(users) == 0 {
		return nil
	}
	return b.addUsers(users, b.inboundTag)
}

func (b *Builder) addUsers(users []*protocol.User, tag string) error {
	inboundManager := b.instance.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := inboundManager.GetHandler(b.ctx, tag)
	if err != nil {
		return fmt.Errorf("failed to get inbound handler: %s", err)
	}

	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s is not a proxy.GetInbound", tag)
	}

	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("inbound handler %s does not implement proxy.UserManager", tag)
	}

	for _, user := range users {
		mUser, err := user.ToMemoryUser()
		if err != nil {
			log.Errorf("failed to create memory user %s: %s", user.Email, err)
			continue
		}
		if err := userManager.AddUser(b.ctx, mUser); err != nil {
			log.Errorf("failed to add user %s: %s", user.Email, err)
		}
	}
	return nil
}

func (b *Builder) removeUsers(users []string, tag string) error {
	inboundManager := b.instance.GetFeature(inbound.ManagerType()).(inbound.Manager)
	handler, err := inboundManager.GetHandler(b.ctx, tag)
	if err != nil {
		return fmt.Errorf("failed to get inbound handler: %s", err)
	}

	inboundInstance, ok := handler.(proxy.GetInbound)
	if !ok {
		return fmt.Errorf("handler %s is not a proxy.GetInbound", tag)
	}

	userManager, ok := inboundInstance.GetInbound().(proxy.UserManager)
	if !ok {
		return fmt.Errorf("inbound handler %s does not implement proxy.UserManager", tag)
	}

	for _, email := range users {
		if err := userManager.RemoveUser(b.ctx, email); err != nil {
			log.Errorf("failed to remove user %s: %s", email, err)
		}
	}
	return nil
}
