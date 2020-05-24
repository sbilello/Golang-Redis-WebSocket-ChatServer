package user

import (
	"errors"
	"fmt"
	"github.com/go-redis/redis/v7"
)

const (
	usersKey       = "users"
	userChannelFmt = "user:%s:channels"
	ChannelsKey    = "channels"
)

type User struct {
	name            string
	channels        []string
	channelsHandler *redis.PubSub

	stopListener    chan struct{}
	listenerRunning bool

	MessageChan chan redis.Message
}

//Connect connect user to user channels on redis
func Connect(rdb *redis.Client, name string) (*User, error) {
	if rdb.SIsMember(usersKey, name).Val() {
		return nil, fmt.Errorf("user %s is already connected", name)
	}
	if _, err := rdb.SAdd(usersKey, name).Result(); err != nil {
		return nil, err
	}

	u := &User{
		name:         name,
		stopListener: make(chan struct{}),
		MessageChan:  make(chan redis.Message),
	}

	if err := u.connect(rdb); err != nil {
		return nil, err
	}

	return u, nil
}

func (u *User) Subscribe(rdb *redis.Client, channel string) error {

	userChannelsKey := fmt.Sprintf(userChannelFmt, u.name)

	if rdb.SIsMember(userChannelsKey, channel).Val() {
		return nil
	}
	if err := rdb.SAdd(userChannelsKey, channel).Err(); err != nil {
		return err
	}

	return u.connect(rdb)
}

func (u *User) Unsubscribe(rdb *redis.Client, channel string) error {

	userChannelsKey := fmt.Sprintf(userChannelFmt, u.name)

	if !rdb.SIsMember(userChannelsKey, channel).Val() {
		return nil
	}
	if err := rdb.SRem(userChannelsKey, channel).Err(); err != nil {
		return err
	}

	return u.connect(rdb)
}

func (u *User) connect(rdb *redis.Client) error {

	var c []string

	c1, err := rdb.SMembers(ChannelsKey).Result()
	if err != nil {
		return err
	}
	c = append(c, c1...)

	// get all user channels (from DB) and start subscribe
	c2, err := rdb.SMembers(fmt.Sprintf(userChannelFmt, u.name)).Result()
	if err != nil {
		return err
	}
	c = append(c, c2...)

	if len(c) == 0 {
		fmt.Println("no channels to connect to for user: ", u.name)
		return nil
	}

	if u.channelsHandler != nil {
		if err := u.channelsHandler.Unsubscribe(); err != nil {
			return err
		}
		if err := u.channelsHandler.Close(); err != nil {
			return err
		}
	}
	if u.listenerRunning {
		close(u.stopListener)
	}

	u.channels = c

	return u.doConnect(rdb)
}

func (u *User) doConnect(rdb *redis.Client) error {
	// subscribe all channels in one request
	pubSub := rdb.Subscribe(u.channels...)
	// keep channel handler to be used in unsubscribe
	u.channelsHandler = pubSub

	// The Listener
	go func() {
		u.listenerRunning = true
		fmt.Println("starting the listener for user:", u.name, "on channels:", u.channels)
		for {
			select {
			case msg, ok := <-pubSub.Channel():
				if !ok {
					break
				}
				u.MessageChan <- *msg

			case <-u.stopListener:
				break
			}
		}
	}()
	return nil
}

func (u *User) Disconnect(rdb *redis.Client) error {
	if u.channelsHandler != nil {
		if err := u.channelsHandler.Unsubscribe(); err != nil {
			return err
		}
		if err := u.channelsHandler.Close(); err != nil {
			return err
		}
	}
	if u.listenerRunning {
		close(u.stopListener)
		u.listenerRunning = false
	}

	if _, err := rdb.SRem(usersKey, u.name).Result(); err != nil {
		return err
	}
	close(u.MessageChan)

	return nil
}

func Chat(rdb *redis.Client, channel string, content string) error {
	return rdb.Publish(channel, content).Err()
}

func List(rdb *redis.Client) ([]string, error) {
	return rdb.SMembers(usersKey).Result()
}

func GetChannels(rdb *redis.Client, username string) ([]string, error) {

	if !rdb.SIsMember(usersKey, username).Val() {
		return nil, errors.New("user not exists")
	}

	var c []string

	c1, err := rdb.SMembers(ChannelsKey).Result()
	if err != nil {
		return nil, err
	}
	c = append(c, c1...)

	// get all user channels (from DB) and start subscribe
	c2, err := rdb.SMembers(fmt.Sprintf(userChannelFmt, username)).Result()
	if err != nil {
		return nil, err
	}
	c = append(c, c2...)

	return c, nil
}
