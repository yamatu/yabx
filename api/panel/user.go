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
			const path = "/api/v1/server/UniProxy/alivelist"
			r, err := c.client.R().
				ForceContentType("application/json").
				Get(path)
			if err != nil || r.StatusCode() >= 399 {
				c.AliveMap.Alive = make(map[int]int)
				return c.AliveMap.Alive, nil
			}
			if r == nil || r.RawResponse == nil {
				fmt.Printf("received nil response or raw response")
				c.AliveMap.Alive = make(map[int]int)
				return c.AliveMap.Alive, nil
			}
			defer r.RawResponse.Body.Close()
			c.AliveMap.Alive = decodeAliveMap(r.Body())

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
		data := make(map[int][]int64, len(userTraffic))
		for i := range userTraffic {
			data[userTraffic[i].UID] = []int64{userTraffic[i].Upload, userTraffic[i].Download}
		}
		const path = "/api/v1/server/UniProxy/push"
		r, err := c.client.R().
			SetBody(data).
			ForceContentType("application/json").
			Post(path)
		err = c.checkResponse(r, path, err)
		if err != nil {
			return err
		}
		return nil
	}

}

func (c *Client) ReportNodeOnlineUsers(data *map[int][]string) error {
	payload := make(map[int][]string)
	if data != nil {
		payload = *data
	}

	post := func(path string, body interface{}) error {
		r, err := c.client.R().
			SetBody(body).
			ForceContentType("application/json").
			Post(path)
		return c.checkResponse(r, path, err)
	}

	wrapper := map[string]map[int][]string{"alive": payload}

	switch c.PanelType {
	case "ppanel":
		const path = "/v1/server/online"
		if err := post(path, payload); err == nil {
			return nil
		}
		return post(path, wrapper)
	default:
		const path = "/api/v1/server/UniProxy/alive"
		if err := post(path, payload); err == nil {
			return nil
		}
		return post(path, wrapper)
	}

}
