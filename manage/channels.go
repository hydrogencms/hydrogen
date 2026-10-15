package manage

import (
	"fmt"
	"net/http"

	"github.com/hydrogencms/hydrogen/channel"
)

// channelJSON is a channel as the API shows it. Tokens are never included. Settings and module options are
// returned in full to every holder of the management token, so they are not secret: keep secrets in module
// config fields tagged `config:"secret"`, never in channel settings or options.
type channelJSON struct {
	Name        string            `json:"name"`
	Group       string            `json:"group"`
	Languages   []string          `json:"languages"`
	Collections []string          `json:"collections"` // null: every collection
	Frontend    string            `json:"frontend"`
	Preview     map[string]string `json:"preview"`
	Access      string            `json:"access"`
	Settings    map[string]any    `json:"settings"`
	Options     map[string]any    `json:"options"`
}

// encodeChannels renders the channel list once; the set is immutable. A nil set lists no channels.
func encodeChannels(set *channel.Set) ([]byte, error) {
	items := []channelJSON{}
	if set != nil {
		for _, c := range set.All() {
			items = append(items, channelJSON{Name: c.Name, Group: c.Group, Languages: c.Languages,
				Collections: c.Collections, Frontend: c.Frontend, Preview: c.Preview, Access: c.Access,
				Settings: c.Settings, Options: c.Options})
		}
	}
	b, err := encode(map[string]any{"items": items})
	if err != nil {
		return nil, fmt.Errorf("manage: encode channels (a channel option value must be JSON-encodable): %w", err)
	}
	return b, nil
}

func (a *api) listChannels(w http.ResponseWriter, _ *http.Request) error {
	a.writeRaw(w, a.channelsJSON)
	return nil
}

func channelSchemas() obj {
	free := obj{"type": "object", "additionalProperties": true}
	return obj{
		"Channel": obj{"type": "object",
			"required":    []any{"name", "group", "languages", "collections", "frontend", "preview", "access", "settings", "options"},
			"description": "A delivery channel from config/channels.yaml. Delivery tokens are never listed.",
			"properties": obj{
				"name": str(""), "group": str(""),
				"languages":   obj{"type": "array", "items": str(""), "description": "Ordered fallback."},
				"collections": obj{"type": []any{"array", "null"}, "items": str(""), "description": "null: every collection."},
				"frontend":    str(""),
				"preview":     obj{"type": "object", "additionalProperties": str(""), "description": "Collection (or *) to URL pattern."},
				"access":      obj{"type": "string", "enum": []any{channel.AccessPublic, channel.AccessToken}},
				"settings":    free,
				"options":     obj{"type": "object", "additionalProperties": true, "description": "Module options by key."},
			}},
		"ChannelList": obj{"type": "object", "required": []any{"items"},
			"properties": obj{"items": arrayOf(schemaRef("Channel"))}},
	}
}
