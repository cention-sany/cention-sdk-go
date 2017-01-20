package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"os"
	"strconv"
	"strings"

	"github.com/cention-sany/cention-sdk-go/cention"
)

var (
	listen, secret, token, endpoint string
	simple                          bool
)

func main() {
	if !flag.Parsed() {
		flag.Parse()
	}
	cention.DEBUG = true
	if listen == "" {
		listen = ":80"
	} else {
		listen = strings.TrimLeft(listen, ":")
		_, err := strconv.Atoi(listen)
		if err != nil {
			fmt.Println("Only support numeric port")
			os.Exit(1)
		}
		listen = ":" + listen
	}
	useSecret := false
	for i, s := range os.Args {
		if i == 0 {
			continue
		}
		if s == "-s" {
			useSecret = true
			break
		}
	}
	err := http.ListenAndServe(listen,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var (
				err error
				cb  *cention.Callback
				ae  *cention.AnswerErrand
				a   *cention.Attachment
			)
			fmt.Println("DEBUG: incoming callback.", useSecret, secret)
			if simple {
				b, _ := httputil.DumpRequest(r, true)
				fmt.Println("DEBUG: data:", string(b))
				return
			}
			if useSecret {
				cb, err = cention.ParseWithSecret(r, secret)
			} else {
				cb, err = cention.Parse(r)
			}
			if err != nil {
				s := fmt.Sprintf(`{"error": %q}`, err)
				fmt.Println("DEBUG error:", s)
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, s)
				return
			}
			fmt.Println("DEBUG:", cb.Data.ID, cb.Data.Type, cb.Data.Attr.Event,
				cb.Meta.Secret)
			// fmt.Println("DEBUG: content", string(cb.Data.Attr.RawMessage))
			ae, err = cb.AnswerErrand()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, `{"error": %q}`, err)
				return
			}
			a, err = ae.GetNextAttachment(context.TODO(), endpoint, token)
			for ; err == nil; a, err = ae.GetNextAttachment(context.TODO(),
				endpoint, token) {
				if err != nil {
					fmt.Println("ERROR get attachment:", err)
				} else {
					fmt.Println("DEBUG attachment:", a.Id, a.Name, a.Ct)
					r, err := a.Content()
					if err != nil {
						fmt.Println("ERROR when getting attachment content:",
							err)
					} else {
						bb, err := ioutil.ReadAll(r)
						if err != nil {
							fmt.Println("ERROR when getting attachment bytes:",
								err)
						} else {
							err = ioutil.WriteFile(a.Name, bb, 0755)
							if err != nil {
								fmt.Println("ERROR when writing attachment bytes into file:",
									a.Name, "because of error:", err)
							} else {
								fmt.Println("DEBUG success written attachment content into file:",
									a.Name)
							}
						}
					}
				}
			}
			if err != nil && err != cention.ErrNoMoreAttachment {
				fmt.Println("ERROR get attachment:", err)
			}
			fmt.Fprintln(w, `{"status":"ok"}`)
		}))
	fmt.Println("ERROR:", err)
}

func init() {
	// command line variables
	flag.StringVar(&listen, "l", "", "Socket port listen to")
	flag.StringVar(&secret, "s", "", "Secret")
	flag.StringVar(&token, "t", "", "Authorized token")
	flag.StringVar(&endpoint, "e", "", "URL endpoint of Cention application")
	flag.BoolVar(&simple, "i", false, "Simple parsing without decode JSON")
}

// SAMPLE - JSONAPI
// {
//   "data": {
//     "type": "c3_callback_answer_errand",
//     "id": "5",
//     "attributes": {
//       "event": 1
//     },
//     "relationships": {
//       "errand": {
//         "data": {
//           "type": "c3_errand",
//           "id": "2238"
//         }
//       }
//     }
//   },
//   "included": [
//     {
//       "type": "c3_responseattachment",
//       "id": "708",
//       "attributes": {
//         "c3_id": 708
//       },
//       "links": {
//         "self": "https:\/\/localhost\/ng\/api\/json\/c3_responseattachment\/708"
//       }
//     },
//     {
//       "type": "c3_responseattachment",
//       "id": "709",
//       "attributes": {
//         "c3_id": 709
//       },
//       "links": {
//         "self": "https:\/\/localhost\/ng\/api\/json\/c3_responseattachment\/709"
//       }
//     },
//     {
//       "type": "c3_areaarchive",
//       "id": "12",
//       "attributes": {
//         "c3_id": 12
//       },
//       "links": {
//         "self": "https:\/\/localhost\/ng\/api\/json\/c3_areaarchive\/12"
//       }
//     },
//     {
//       "type": "c3_errand",
//       "id": "2238",
//       "attributes": {
//         "answer": {
//           "c3_id": 1171,
//           "response": {
//             "body": "Plain text string",
//             "c3_id": 3267,
//             "htmlBody": "<div style=\"font-size:;font-family:;\"><div>HTML text string<\/div>",
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
//         "c3_id": 2238,
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
//               "type": "c3_responseattachment",
//               "id": "708"
//             },
//             {
//               "type": "c3_responseattachment",
//               "id": "709"
//             }
//           ]
//         },
//         "embedded_archives": {
//           "data": [
//             {
//               "type": "c3_areaarchive",
//               "id": "12"
//             }
//           ]
//         }
//       }
//     }
//   ],
//   "meta": {
//     "api_secret": "123456"
//   }
// }
