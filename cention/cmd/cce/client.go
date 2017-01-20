package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/cention-sany/cention-sdk-go/cention"
)

var (
	withAttachment  bool
	token, endpoint string
)

func main() {
	if !flag.Parsed() {
		flag.Parse()
	}
	cention.DEBUG = true
	if endpoint == "" || token == "" {
		fmt.Println("Please supply both endpoint and token")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	go func() {
		m := new(cention.Message)
		m.Message_id = "msgid_" + strconv.Itoa(int(time.Now().Unix()))
		m.Name = "Sany Liew"
		m.From = "sany.liew@test.com.my"
		m.Subject = "Creating test errand via API"
		m.Body = fmt.Sprintf("Test message body at %s",
			time.Now().Format(time.RFC1123Z))
		if withAttachment {
			a1, _ := cention.CreateAttachment("tst1.txt", "text/plain",
				bytes.NewReader([]byte(`Apple Pen Pineapple Pen`)))
			a2, _ := cention.CreateAttachment("tst2.txt", "text/plain",
				bytes.NewReader([]byte(`Orange Juice!`)))
			m.As = []*cention.Attachment{a1, a2}
		}
		resp, err := cention.CreateErrand(ctx, endpoint, token, m)
		if err != nil {
			fmt.Println("ERROR:", err)
			os.Exit(1)
		}
		fmt.Printf("RESPONSE: %v\n", resp)
		cancel()
	}()
	<-ctx.Done()
}

func init() {
	// command line variables
	flag.StringVar(&token, "t", "", "Authorized token")
	flag.StringVar(&endpoint, "e", "", "URL endpoint of Cention application")
	flag.BoolVar(&withAttachment, "a", false, "Enable attachment sending")
}
