// cention provides the func to communicate with Cention Application through
// JSONAPI.
package cention

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/cention-sany/jsonapi"
	"github.com/mitchellh/mapstructure"
)

var (
	DEBUG               bool
	ErrUnmatchEventType = errors.New("cention: event type not match")
	ErrSecretFail       = errors.New("cention: secret check failed")
	ErrNoMoreAttachment = errors.New("cention: no more attachment")
)

type event int

const (
	ANSWER_ERRAND event = 1
)

const (
	normalAttachment = iota
	areaArchiveAttachment
)

const (
	jsonapiMime = "application/vnd.api+json"
	rwPrfx      = "/capi"
)

type CreatedResponse struct {
	Data struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"data"`
}

type CE struct {
	Data struct {
		Type string `json:"type"`
		Attr `json:"attributes"`
	} `json:"data"`
}

type Attr struct {
	Msg *Message `json:"msg"`
	Ans *Answer  `json:"answer,omitempty"`
}

type Message struct {
	Message_id string        `json:"message_id"`
	Name       string        `json:"name"`
	From       string        `json:"from"`
	Subject    string        `json:"subject"`
	Body       string        `json:"body"`
	HtmlBody   string        `json:"html_body"`
	As         []*Attachment `json:"attachments,omitempty"`
}

type Answer struct {
	Message
	UserType string `json:"user_type"`
	UserID   string `json:"user_id"`
}

const (
	ceJT = "c3_new_errands"
	aeJT = "c3_answer_errands"
	raJT = "c3_response_attachments"
	aaJT = "c3_area_archives"
)

var (
	ceREST            = fmt.Sprint(rwPrfx, "/json/", ceJT)
	getAttachmentREST = fmt.Sprint(rwPrfx, "/json/", raJT)
	getAAREST         = fmt.Sprint(rwPrfx, "/json/", aaJT)
)

func CreateErrand(ctx context.Context, ep, tk string,
	m *Message, ans *Answer) (*CreatedResponse, error) {
	var ce CE
	ce.Data.Type = ceJT
	for _, a := range m.As {
		a.Id = 0 // errand creation can not has id
	}
	ce.Data.Msg = m
	ce.Data.Ans = ans
	b := new(bytes.Buffer)
	err := json.NewEncoder(b).Encode(&ce)
	if err != nil {
		return nil, err
	}
	ep = strings.TrimRight(ep, "/")
	var (
		bs *bytes.Buffer
		r  io.Reader
	)
	if DEBUG {
		bs = new(bytes.Buffer)
		r = io.TeeReader(b, bs)
	} else {
		r = b
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprint(ep, ceREST),
		ioutil.NopCloser(r))
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", jsonapiMime)
	req.Header.Set("Authorization", fmt.Sprint("Bearer ", tk))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if DEBUG {
		bb, err := httputil.DumpRequestOut(req, false)
		if err != nil {
			return nil, err
		}
		fmt.Print("Request header:", string(bb))
		fmt.Println("Request body:", string(bs.Bytes()))
	}
	if DEBUG {
		bs.Reset()
		r = io.TeeReader(resp.Body, bs)
	} else {
		r = resp.Body
	}
	created := &CreatedResponse{}
	err = json.NewDecoder(r).Decode(created)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}
	if DEBUG {
		fmt.Println("Response", resp.Status)
		fmt.Println("Response body:", string(bs.Bytes()))
	}
	resp.Body.Close()
	return created, nil
}

type Callback struct {
	*jsonapi.OnePayload
	Meta *struct {
		Secret string `json:"api_secret"`
	} `json:"meta"`
}

func (c *Callback) AnswerErrand() (*AnswerErrand, error) {
	v, existOrOk := c.OnePayload.Data.Attributes["event"]
	if !existOrOk {
		return nil, ErrUnmatchEventType
	}
	var i float64
	i, existOrOk = v.(float64)
	if !existOrOk {
		return nil, ErrUnmatchEventType
	}
	if int(i) != int(ANSWER_ERRAND) {
		return nil, ErrUnmatchEventType
	}
	var a *AnswerErrand
	nodes := c.OnePayload.Included
	as := make([]*attachmentNode, 0, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case aeJT:
			a = new(AnswerErrand)
			if err := mapstructure.Decode(n.Attributes, a); err != nil {
				return nil, err
			}
		case raJT:
			addAttachment(&as, normalAttachment, n)
		case aaJT:
			addAttachment(&as, areaArchiveAttachment, n)
		}
	}
	if a == nil {
		return nil, errors.New("cention: no errand found")
	} else if len(as) > 0 {
		a.as = as
	}
	return a, nil
}

func addAttachment(an *[]*attachmentNode, t int, n *jsonapi.Node) {
	var s string
	if v, ok := (*n.Links)["self"]; ok {
		if s, ok = v.(string); !ok {
			link, _ := v.(jsonapi.Link)
			s = link.Href
		}
	}
	*an = append(*an, &attachmentNode{
		id: n.ID, t: t, l: s,
	})
}

type AnswerErrand struct {
	ID     int `mapstructure:"c3_id"`
	Answer struct {
		ID       int `mapstructure:"c3_id"`
		Response struct {
			ID       int    `mapstructure:"c3_id"`
			Body     string `mapstructure:"body"`
			HtmlBody string `mapstructure:"html_body"`
			Subject  string `mapstructure:"subject"`
			To       []struct {
				ID      int    `mapstructure:"c3_id"`
				Address string `mapstructure:"email_address"`
				Name    string `mapstructure:"name"`
			} `mapstructure:"to"`
		} `mapstructure:"response"`
	} `mapstructure:"answer"`
	as  []*attachmentNode
	err error
}

type attachmentNode struct {
	t     int
	id, l string
}

func (a attachmentNode) get(ctx context.Context, ep, tk string) (io.ReadCloser,
	error) {
	if a.l == "" {
		switch a.t {
		case normalAttachment:
			return req(ctx, a.id, ep, tk, getAttachmentREST)
		case areaArchiveAttachment:
			return req(ctx, a.id, ep, tk, getAAREST)
		default:
			return nil, errors.New("cention: unknown attachment type")
		}
	}
	return httpReq(ctx, a.l, tk)
}

func httpReq(ctx context.Context, ep, tk string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, ep, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", jsonapiMime)
	req.Header.Set("Authorization", fmt.Sprint("Bearer ", tk))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func req(ctx context.Context, id, ep, tk, p string) (io.ReadCloser, error) {
	return httpReq(ctx, fmt.Sprint(ep, p, "/", id), tk)
}

func (a *AnswerErrand) MoreAttachment() (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	if len(a.as) == 0 {
		return false, nil
	}
	return true, nil
}

// GetNextAttachment will retrieve attachment data from Cention server by
// providing its endpoint ep and JWT token tk. Only successfully call will
// advance attachment index.
func (a *AnswerErrand) GetNextAttachment(ctx context.Context, ep,
	tk string) (*Attachment, error) {
	if b, err := a.MoreAttachment(); err != nil {
		return nil, err
	} else if !b {
		return nil, ErrNoMoreAttachment
	}
	rc, err := a.as[0].get(ctx, ep, tk)
	if err != nil {
		a.err = err
		return nil, err
	}
	defer rc.Close()
	v := new(jsonapi.OnePayload)
	if err = json.NewDecoder(rc).Decode(v); err != nil {
		a.err = err
		return nil, err
	}
	vv, exist := v.Data.Attributes["attachment"]
	if !exist {
		return nil, errors.New("cention: no attachment key return")
	}
	m, ok := vv.(map[string]interface{})
	if !ok {
		return nil, errors.New("cention: no attachment map found")
	}
	aa := new(Attachment)
	if err = mapstructure.Decode(m, aa); err != nil {
		a.err = err
		return nil, err
	}
	a.as = a.as[1:]
	return aa, nil
}

type Attachment struct {
	Id         int    `json:"c3_id,omitempty" mapstructure:"c3_id"`
	Ct         string `json:"content_type" mapstructure:"content_type"`
	Name       string `json:"name" mapstructure:"name"`
	B64Content string `json:"content" mapstructure:"content"`
	CID        string `json:"content_id,omitempty" mapstructure:"content_id"`
}

func (a *Attachment) Content() (io.Reader, error) {
	return base64.NewDecoder(base64.StdEncoding,
		bytes.NewReader([]byte(a.B64Content))), nil
}

// CreateAttachment is a helper function to create Attachment object. User can
// directly mutate the Attachment field. name is attachment's filename. ct is
// attachment content type and r is the content of attachment.
func CreateAttachment(name, ct string, r io.Reader) (*Attachment, error) {
	a := new(Attachment)
	a.Name = name
	a.Ct = ct
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	a.B64Content = base64.StdEncoding.EncodeToString(b)
	return a, nil
}

func parseSecret(r *http.Request, check bool,
	secret string) (*Callback, error) {
	defer r.Body.Close()
	v := new(Callback)
	err := json.NewDecoder(r.Body).Decode(v)
	if err != nil {
		return nil, err
	}
	if check && (v.Meta == nil || v.Meta.Secret != secret) {
		return nil, ErrSecretFail
	}
	return v, nil
}

func Parse(r *http.Request) (*Callback, error) {
	return parseSecret(r, false, "")
}

func ParseWithSecret(r *http.Request, s string) (*Callback, error) {
	return parseSecret(r, true, s)
}

// Sample create errand data
// {
//   "data": {
//     "type": "c3_new_errands",
//     "attributes": {
//       "msg": {
//         "message_id": "msgid_1485096554",
//         "name": "Sany Liew",
//         "from": "sany.liew@test.com.my",
//         "subject": "Creating test errand via API",
//         "body": "Test message body at Sun, 22 Jan 2017 22:49:14 +0800",
//         "htmlBody": "",
//         "attachments": [
//           {
//             "content_type": "text\/plain",
//             "name": "tst1.txt",
//             "content": "QXBwbGUgUGVuIFBpbmVhcHBsZSBQZW4="
//           },
//           {
//             "content_type": "text\/plain",
//             "name": "tst2.txt",
//             "content": "T3JhbmdlIEp1aWNlIQ=="
//           }
//         ]
//       }
//     }
//   }
// }

// SAMPLE - JSONAPI callback
// {
//   "data": {
//     "type": "c3_callback_answer_errands",
//     "id": "76",
//     "attributes": {
//       "event": 1
//     },
//     "relationships": {
//       "errand": {
//         "data": {
//           "type": "c3_answer_errands",
//           "id": "2256"
//         }
//       }
//     }
//   },
//   "included": [
//     {
//       "type": "c3_response_attachments",
//       "id": "789",
//       "attributes": {
//         "c3_id": 789
//       },
//       "links": {
//         "self": "http:\/\/localhost\/ng\/api\/json\/c3_response_attachments\/789"
//       }
//     },
//     {
//       "type": "c3_response_attachments",
//       "id": "790",
//       "attributes": {
//         "c3_id": 790
//       },
//       "links": {
//         "self": "http:\/\/localhost\/ng\/api\/json\/c3_response_attachments\/790"
//       }
//     },
//     {
//       "type": "c3_area_archives",
//       "id": "12",
//       "attributes": {
//         "c3_id": 12
//       },
//       "links": {
//         "self": {
//           "href": "http:\/\/localhost\/ng\/api\/json\/c3_area_archives\/12"
//         }
//       }
//     },
//     {
//       "type": "c3_answer_errands",
//       "id": "2256",
//       "attributes": {
//         "answer": {
//           "c3_id": 1189,
//           "response": {
//             "body": "text",
//             "c3_id": 3304,
//             "htmlBody": "<div style=\"font-size:;font-family:;\"><div>sadadad<img src=\"cid:12\" \/><img alt=\"\" src=\"cid:attachment_788\" \/><\/div>\n\n<br \/><br \/><div>\u00a0<\/div>\r\n<a href=\"http:\/\/localhost\/errands\/satisfaction\/meter\/-\/answer\/4d6a49314e673d3d\/1b7f046991d401785264fc4d35ca3db6\" style=\"font-family:Verdana; font-size:10pt; color:black; font-style:normal;\">optionONE<\/a><br \/><a href=\"http:\/\/localhost\/errands\/satisfaction\/meter\/-\/answer\/4d6a49314e673d3d\/73d05e8f6ec0ca16963a6608b78debd9\" style=\"font-family:Verdana; font-size:10pt; color:black; font-style:normal;\">optionTWO<\/a><br \/><br \/>\n--------- Cention Contact Center - Original message ---------<br \/>\nErrand: #2256-1<br \/>\nFrom: Sany Liew (sany.liew@test.com.my)<br \/>\nSent: 2017\/01\/22 16:53<br \/>\nTo: test-services-area<br \/>\nSubject: Creating test errand via API<br \/>\nQuestion: <br \/>\n<br \/>\nTest message body at Sun, 22 Jan 2017 16:53:04 +0800<br \/>\n<br \/>--------- Cention Contact Center - Original message - End ---------<\/div>",
//             "subject": "Creating test errand via API",
//             "to": [
//               {
//                 "c3_id": 279,
//                 "emailAddress": "sany.liew@test.com.my",
//                 "name": "Sany Liew"
//               }
//             ]
//           }
//         },
//         "c3_id": 2256,
//         "service": {
//           "c3_id": 16,
//           "name": "Form",
//           "type": 19
//         }
//       },
//       "relationships": {
//         "attachments": {
//           "data": [
//             {
//               "type": "c3_response_attachments",
//               "id": "788"
//             },
//             {
//               "type": "c3_response_attachments",
//               "id": "789"
//             },
//             {
//               "type": "c3_response_attachments",
//               "id": "790"
//             }
//           ]
//         },
//         "embedded_archives": {
//           "data": [
//             {
//               "type": "c3_area_archives",
//               "id": "12"
//             }
//           ]
//         }
//       }
//     },
//     {
//       "type": "c3_response_attachments",
//       "id": "788",
//       "attributes": {
//         "c3_id": 788
//       },
//       "links": {
//         "self": "http:\/\/localhost\/ng\/api\/json\/c3_response_attachments\/788"
//       }
//     }
//   ],
//   "meta": {
//     "api_secret": "123456"
//   }
// }
