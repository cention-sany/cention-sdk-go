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
	"strconv"
	"strings"
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

type CreatedResponse struct {
	Data struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"data"`
}

type GetJSONAPI struct {
	Data struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		json.RawMessage `json:"attributes"`
	} `json:"data"`
}

type CE struct {
	Data struct {
		Type string `json:"type"`
		Attr `json:"attr"`
	} `json:"data"`
}

type Attr struct {
	Msg *Message `json:"msg"`
}

type Message struct {
	Message_id string        `json:"message_id"`
	Name       string        `json:"name"`
	From       string        `json:"from"`
	Subject    string        `json:"subject"`
	Body       string        `json:"body"`
	HtmlBody   string        `json:"htmlBody"`
	As         []*Attachment `json:"attachments,omitempty"`
}

const (
	jsonapiMime       = "application/vnd.api+json"
	ceREST            = "/ng/api/json/c3_errand"
	getAttachmentREST = "/ng/api/json/c3_responseattachment"
	getAAREST         = "/ng/api/json/c3_areaarchive"
)

func CreateErrand(ctx context.Context, ep, tk string,
	m *Message) (*CreatedResponse, error) {
	var ce CE
	ce.Data.Type = "c3_errand"
	for _, a := range m.As {
		a.Id = 0 // errand creation can not has id
	}
	ce.Data.Msg = m
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
	Data struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Attr struct {
			Event           int `json:"event"`
			json.RawMessage `json:"event_data"`
			XData           json.RawMessage `json:"event_xdata"`
		} `json:"attributes"`
	} `json:"data"`
	Meta *struct {
		Secret string `json:"api_secret"`
	} `json:"meta"`
}

func (c *Callback) AnswerErrand() (*AnswerErrand, error) {
	if c.Data.Attr.Event != int(ANSWER_ERRAND) {
		return nil, ErrUnmatchEventType
	}
	v := new(AnswerErrand)
	if err := json.Unmarshal((c.Data.Attr.RawMessage), v); err != nil {
		return nil, err
	}
	if len(c.Data.Attr.XData) > 0 {
		vv := new(struct {
			As attachmentSlice `json:"embedded_archives"`
		})
		if err := json.Unmarshal([]byte(c.Data.Attr.XData),
			vv); err != nil {
			return nil, err
		}
		v.areaArchive = vv.As
	}
	return v, nil
}

type AnswerErrand struct {
	ID     int `json:"c3_id"`
	Answer struct {
		ID       int `json:"c3_id"`
		Response struct {
			ID       int    `json:"c3_id"`
			Body     string `json:"body"`
			HtmlBody string `json:"htmlBody"`
			Subject  string `json:"subject"`
			To       []struct {
				ID      int    `json:"c3_id"`
				Address string `json:"emailAddress"`
				Name    string `json:"name"`
			} `json:"to"`
			As attachmentSlice `json:"attachments"`
		} `json:"response"`
	} `json:"answer"`
	areaArchive         attachmentSlice
	at                  *attachmentSlice
	rq                  attachmentGetter
	currAttachmentIndex int
	err                 error
}

type attachmentSlice []struct {
	ID int `json:"c3_id"`
}

type attachmentGetter interface {
	get(context.Context, int, string, string) (io.ReadCloser, error)
	next() bool
}

func newNormAttachment() normAttachment {
	return normAttachment{}
}

type normAttachment struct{}

func (normAttachment) get(ctx context.Context, n int, ep,
	tk string) (io.ReadCloser, error) {
	return req(ctx, n, ep, tk, getAttachmentREST)
}

func (normAttachment) next() bool {
	return true
}

func newAreaArchive() areaArchive {
	return areaArchive{}
}

type areaArchive struct{}

func (areaArchive) get(ctx context.Context, n int, ep,
	tk string) (io.ReadCloser, error) {
	return req(ctx, n, ep, tk, getAAREST)
}

func (areaArchive) next() bool {
	return false
}

func req(ctx context.Context, n int, ep, tk, p string) (io.ReadCloser, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprint(ep, p, "/",
		strconv.Itoa(n)), nil)
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

func (a *AnswerErrand) MoreAttachment() (bool, error) {
	if a.err != nil {
		return false, a.err
	}
	as := len(a.Answer.Response.As)
	aas := len(a.areaArchive)
	if as == 0 && aas == 0 {
		return false, nil
	}
	if a.at == nil {
		if as != 0 {
			a.at = &a.Answer.Response.As
			a.rq = newNormAttachment()
			if DEBUG {
				fmt.Println("Normal Attachment:", a.at)
			}
		} else {
			a.at = &a.areaArchive
			a.rq = newAreaArchive()
			if DEBUG {
				fmt.Println("Area Archive:", a.at)
			}
		}
	} else {
		if a.currAttachmentIndex >= len(*a.at) {
			if a.at == &a.areaArchive || aas == 0 {
				a.areaArchive = nil
				return false, nil
			}
			a.Answer.Response.As = nil
			a.at = &a.areaArchive
			a.rq = newAreaArchive()
			if DEBUG {
				fmt.Println("Area Archive:", a.at)
			}
			a.currAttachmentIndex = 0
		}
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
	rc, err := a.rq.get(ctx, (*a.at)[a.currAttachmentIndex].ID, ep, tk)
	if err != nil {
		a.err = err
		return nil, err
	}
	defer rc.Close()
	v := new(GetJSONAPI)
	if err = json.NewDecoder(rc).Decode(v); err != nil {
		a.err = err
		return nil, err
	}
	vv := new(struct {
		*Attachment `json:"attachment"`
	})
	if err = json.Unmarshal([]byte(v.Data.RawMessage), vv); err != nil {
		a.err = err
		return nil, err
	}
	a.currAttachmentIndex++
	return vv.Attachment, nil
}

type Attachment struct {
	Id         int    `json:"c3_id,omitempty"`
	Ct         string `json:"content_type"`
	Name       string `json:"name"`
	B64Content string `json:"content"`
	CID        string `json:"content_id,omitempty"`
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
