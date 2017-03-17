package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cention-sany/cention-sdk-go/cention"
)

const maxSafeRq = 50

var (
	concurrent                 int
	withAttachment, insaneTest bool
	token, endpoint, closeUser string

	wg sync.WaitGroup
)

func main() {
	t := time.Now()
	if !flag.Parsed() {
		flag.Parse()
	}
	cention.DEBUG = true
	if endpoint == "" || token == "" {
		fmt.Println("Please supply both endpoint and token")
		os.Exit(1)
	}
	totalRq := concurrent
	if !insaneTest {
		if totalRq > maxSafeRq {
			totalRq = maxSafeRq
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(),
		time.Minute*time.Duration(totalRq))
	status := make(chan bool)
	doneReport := make(chan struct{})
	go func() {
		var success, failed int
		for ok := range status {
			if ok {
				success++
			} else {
				failed++
			}
		}
		fmt.Printf("Total #%d. Success #%d. Failed #%d\n", totalRq,
			success, failed)
		fmt.Printf("Total time passed: %.3f\n", time.Since(t).Seconds())
		close(doneReport)
	}()
	wg.Add(totalRq)
	for i := 1; i <= totalRq; i++ {
		go func(i int) {
			defer wg.Done()
			m := new(cention.Message)
			m.Message_id = fmt.Sprint("msgid_", i, "_",
				strconv.Itoa(int(time.Now().Unix())))
			m.Name = "Sany Liew"
			m.From = fmt.Sprintf("sany.liew.%d@test.com.my", i)
			m.Subject = fmt.Sprint("Creating test errand via API #", i)
			m.Body = fmt.Sprintf("#%d Test message body at %s",
				i, time.Now().Format(time.RFC1123Z))
			if withAttachment {
				a1, _ := cention.CreateAttachment(fmt.Sprintf("tst1_%d.txt", i),
					"text/plain",
					bytes.NewReader([]byte(`Apple Pen Pineapple Pen`)))
				a2, _ := cention.CreateAttachment(fmt.Sprintf("tst2_%d.txt", i),
					"text/plain",
					bytes.NewReader([]byte(`Orange Juice!`)))
				m.As = []*cention.Attachment{a1, a2}
			}
			var a *cention.Answer
			if closeUser != "" {
				n, _ := strconv.Atoi(closeUser)
				a = new(cention.Answer)
				a.Subject = "This is subject for ANSWER"
				a.Body = "This is plain answer text"
				if n >= 0 {
					a.UserID = strconv.Itoa(n)
					a.UserType = "CENTION"
				}
			}
			resp, err := cention.CreateErrand(ctx, endpoint, token, m, a)
			if err != nil {
				fmt.Println("Rq #:", i, "ERROR:", err)
				select {
				case <-ctx.Done():
				case status <- false:
				}
			} else {
				fmt.Printf("Rq #:%d RESPONSE: %v\n", i, resp)
				select {
				case <-ctx.Done():
				case status <- true:
				}
			}
		}(i)
	}
	go func() {
		wg.Wait()
		cancel()
		close(status)
	}()
	<-doneReport
	<-ctx.Done()
}

func init() {
	// command line variables
	flag.StringVar(&token, "t", "", "Authorized token")
	flag.StringVar(&endpoint, "e", "", "URL endpoint of Cention application")
	flag.IntVar(&concurrent, "c", 1, "Number of requests concurrently")
	flag.BoolVar(&withAttachment, "a", false, "Enable attachment sending")
	flag.BoolVar(&insaneTest, "insane", false, "")
	flag.StringVar(&closeUser, "u", "", "Create errand that will be closed by this user ID (-1 will be closed by system)")
}
