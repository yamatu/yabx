package panel

import (
	"fmt"
	"strconv"

	"github.com/goccy/go-json"
)

type OnlineUser struct {
	UID int
	IP  string
}

type UserInfo struct {
	Id          int    `json:"id"`
	Uuid        string `json:"uuid"`
	SpeedLimit  int    `json:"speed_limit"`
	DeviceLimit int    `json:"device_limit"`
}

type UserListBody struct {
	//Msg  string `json:"msg"`
	Users []UserInfo `json:"users"`
}

type AliveMap struct {
	Alive map[int]int `json:"alive"`
}

func decodeAliveMap(body []byte) map[int]int {
	alive := make(map[int]int)

	wrapped := &AliveMap{}
	if err := json.Unmarshal(body, wrapped); err == nil && wrapped.Alive != nil {
		return wrapped.Alive
	}

	directInt := make(map[int]int)
	if err := json.Unmarshal(body, &directInt); err == nil && len(directInt) > 0 {
		return directInt
	}

	directStr := make(map[string]int)
	if err := json.Unmarshal(body, &directStr); err == nil && len(directStr) > 0 {
		for k, v := range directStr {
			if uid, err := strconv.Atoi(k); err == nil {
				alive[uid] = v
			}
		}
		return alive
	}

	directAny := make(map[string]interface{})
	if err := json.Unmarshal(body, &directAny); err == nil && len(directAny) > 0 {
		for k, v := range directAny {
			uid, err := strconv.Atoi(k)
			if err != nil {
				continue
			}
			switch n := v.(type) {
			case float64:
				alive[uid] = int(n)
			case string:
				if iv, err := strconv.Atoi(n); err == nil {
					alive[uid] = iv
				}
			}
		}
		if len(alive) > 0 {
			return alive
		}
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err == nil {
		if raw, ok := payload["alive"]; ok {
			decoded := decodeAliveMap(raw)
			if len(decoded) > 0 {
				return decoded
			}
		}
		if raw, ok := payload["data"]; ok {
			decoded := decodeAliveMap(raw)
			if len(decoded) > 0 {
				return decoded
			}
		}
	}

	return alive
}

func (c *Client) fetchAliveList(paths ...string) map[int]int {
	for _, path := range paths {
		r, err := c.client.R().
			ForceContentType("application/json").
			Get(path)
		if err != nil || r == nil || r.RawResponse == nil {
			continue
		}
		if r.StatusCode() >= 399 {
			_ = r.RawResponse.Body.Close()
			continue
		}
		_ = r.RawResponse.Body.Close()
		return decodeAliveMap(r.Body())
	}

	return make(map[int]int)
}

// GetUserList will pull user from v2board
func (c *Client) GetUserList() ([]UserInfo, error) {
	switch c.PanelType {
	case "ppanel":
		{
			const path = "/v1/server/user"
			r, err := c.client.R().
				SetHeader("If-None-Match", c.userEtag).
				ForceContentType("application/json").
				Get(path)
			if r == nil || r.RawResponse == nil {
				return nil, fmt.Errorf("received nil response or raw response")
			}
			defer r.RawResponse.Body.Close()

			if r.StatusCode() == 304 {
				return nil, nil
			}

			if err = c.checkResponse(r, path, err); err != nil {
				return nil, err
			}
			userlist := &UserListBody{}
			if err := json.Unmarshal(r.Body(), userlist); err != nil {
				return nil, fmt.Errorf("unmarshal user list error: %w", err)
			}
			c.UserList = userlist
			c.userEtag = r.Header().Get("ETag")
			return userlist.Users, nil
		}
	default:
		{
			const path = "/api/v1/server/UniProxy/user"
			r, err := c.client.R().
				SetHeader("If-None-Match", c.userEtag).
				ForceContentType("application/json").
				Get(path)
			if r == nil || r.RawResponse == nil {
				return nil, fmt.Errorf("received nil response or raw response")
			}
			defer r.RawResponse.Body.Close()

			if r.StatusCode() == 304 {
				return nil, nil
			}

			if err = c.checkResponse(r, path, err); err != nil {
				return nil, err
			}
			userlist := &UserListBody{}
			if err := json.Unmarshal(r.Body(), userlist); err != nil {
				return nil, fmt.Errorf("unmarshal user list error: %w", err)
			}
			c.UserList = userlist
			c.userEtag = r.Header().Get("ETag")
			return userlist.Users, nil
		}
	}

}

// GetUserAlive will fetch the alive_ip count for users
func (c *Client) GetUserAlive() (map[int]int, error) {
	switch c.PanelType {
	case "ppanel":
		{
			c.AliveMap = &AliveMap{}
			c.AliveMap.Alive = make(map[int]int)
			/*const path = "/v1/server/alivelist"
			r, err := c.client.R().
				ForceContentType("application/json").
				Get(path)
			if err != nil || r.StatusCode() >= 399 {
				c.AliveMap.Alive = make(map[int]int)
			}
			if r == nil || r.RawResponse == nil {
				fmt.Printf("received nil response or raw response")
				c.AliveMap.Alive = make(map[int]int)
			}
			defer r.RawResponse.Body.Close()
			if err := json.Unmarshal(r.Body(), c.AliveMap); err != nil {
				//fmt.Printf("unmarshal user alive list error: %s", err)
				c.AliveMap.Alive = make(map[int]int)
			}
			*/
			return c.AliveMap.Alive, nil
		}
	default:
		{
			c.AliveMap = &AliveMap{}
			c.AliveMap.Alive = c.fetchAliveList(
				"/api/v1/server/UniProxy/alivelist",
				"/api/v2/server/alivelist",
				"/api/v1/server/alivelist",
				"/api/v2/server/alivelist",
			)

			return c.AliveMap.Alive, nil
		}
	}

}

type ServerPushUserTrafficRequest struct {
	Traffic []UserTraffic `json:"traffic"`
}

type UserTraffic struct {
	UID      int
	Upload   int64
	Download int64
}

// ReportUserTraffic reports the user traffic
func (c *Client) ReportUserTraffic(userTraffic []UserTraffic) error {
	switch c.PanelType {

	case "ppanel":
		traffic := make([]UserTraffic, 0)
		for i := range userTraffic {
			traffic = append(traffic, UserTraffic{
				UID:      userTraffic[i].UID,
				Upload:   userTraffic[i].Upload,
				Download: userTraffic[i].Download,
			})
		}
		path := "/v1/server/push"
		req := ServerPushUserTrafficRequest{
			Traffic: traffic,
		}
		r, err := c.client.R().
			SetBody(req).
			ForceContentType("application/json").
			Post(path)
		err = c.checkResponse(r, path, err)
		if err != nil {
			return err
		}
		return nil
	default:
		data := make(map[string][]int64, len(userTraffic))
		for i := range userTraffic {
			data[strconv.Itoa(userTraffic[i].UID)] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
		}

		paths := []string{
			"/api/v1/server/UniProxy/push",
			"/api/v2/server/push",
			"/api/v1/server/push",
		}
		var lastErr error
		for _, path := range paths {
			r, err := c.client.R().
				SetBody(data).
				ForceContentType("application/json").
				Post(path)
			if err = c.checkResponse(r, path, err); err == nil {
				return nil
			}
			lastErr = err
		}
		return lastErr
	}

}

func (c *Client) ReportNodeOnlineUsers(data *map[int][]string) error {
	payload := make(map[int][]string)
	if data != nil {
		payload = *data
	}

	idPayload := make(map[string][]string, len(payload))
	for uid, ips := range payload {
		idPayload[strconv.Itoa(uid)] = ips
	}

	uidToUUID := make(map[int]string)
	if c.UserList != nil {
		for _, u := range c.UserList.Users {
			if u.Id > 0 && u.Uuid != "" {
				uidToUUID[u.Id] = u.Uuid
			}
		}
	}

	mixedPayload := make(map[string][]string, len(idPayload)*2)
	for k, v := range idPayload {
		mixedPayload[k] = v
	}
	for uid, ips := range payload {
		if uuid, ok := uidToUUID[uid]; ok {
			mixedPayload[uuid] = ips
		}
	}

	post := func(path string, body interface{}) error {
		r, err := c.client.R().
			SetBody(body).
			ForceContentType("application/json").
			Post(path)
		return c.checkResponse(r, path, err)
	}

	idWrapper := map[string]map[string][]string{"alive": idPayload}
	mixedWrapper := map[string]map[string][]string{"alive": mixedPayload}

	switch c.PanelType {
	case "ppanel":
		const path = "/v1/server/online"
		if err := post(path, idPayload); err == nil {
			return nil
		}
		if err := post(path, idWrapper); err == nil {
			return nil
		}
		if len(mixedPayload) != len(idPayload) {
			if err := post(path, mixedPayload); err == nil {
				return nil
			}
			if err := post(path, mixedWrapper); err == nil {
				return nil
			}
		}
		return fmt.Errorf("report online users failed on %s", c.assembleURL(path))
	default:
		paths := []string{
			"/api/v1/server/UniProxy/alive",
			"/api/v2/server/alive",
			"/api/v1/server/online",
			"/api/v2/server/online",
		}
		var lastErr error
		for _, path := range paths {
			if err := post(path, idPayload); err == nil {
				return nil
			} else {
				lastErr = err
			}
			if err := post(path, idWrapper); err == nil {
				return nil
			} else {
				lastErr = err
			}
			if len(mixedPayload) != len(idPayload) {
				if err := post(path, mixedPayload); err == nil {
					return nil
				} else {
					lastErr = err
				}
				if err := post(path, mixedWrapper); err == nil {
					return nil
				} else {
					lastErr = err
				}
			}
		}
		return lastErr
	}

}
