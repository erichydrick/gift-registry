package server

import (
	"bytes"
	"fmt"
	"net/smtp"
	"text/template"
)

type Emailer interface {
	SendVerificationEmail(to []string, code string, getenv func(string) string) error
}

type emailSender struct {
	fromAddress string
	hostname    string
	passwd      string
	port        string
}

type loginEmail struct {
	Code string
	From string
	To   []string
}

var (
	sender Emailer = nil
)

func SetupEmailer(getenv func(string) string) Emailer {

	/*
		Re-use the existing email sender if we have one.
	*/
	if sender == nil {

		sender = &emailSender{
			fromAddress: getenv("EMAIL_FROM"),
			hostname:    getenv("EMAIL_HOST"),
			passwd:      getenv("EMAIL_PASS"),
			port:        getenv("EMAIL_PORT"),
		}

	}

	return sender

}

// Returns a string representation of the emailer
func (es emailSender) String() string {
	return fmt.Sprintf("fromAddress=%s, hostname=%s, passwd=******, port=%s", es.fromAddress, es.hostname, es.port)
}

// Send the login email to the given address used for registering an account
// to confirm the poerson who tried to log in is the person who owns the
// address.
func (es *emailSender) SendVerificationEmail(to []string, code string, getenv func(string) string) error {

	const subject = "Subject: Your login code for the gift registry"
	const mime = "MIME-version: 1.0;\nContent-Type: text/html; charset=\"UTF-8\";"

	/* Build the data for the email body */
	fields := loginEmail{
		Code: code,
	}

	templates := getenv("TEMPLATES_DIR")
	tmpl, err := template.ParseFiles(templates + "/login_email.html")

	if err != nil {
		return fmt.Errorf("could not load email template: %v", err)
	}

	msg := new(bytes.Buffer)
	if _, err = fmt.Fprintf(msg, "%s\n%s\n\n", subject, mime); err != nil {
		return fmt.Errorf("error writing the message subject and mime type to buffer: %v", err)
	}

	if err = tmpl.ExecuteTemplate(msg, "login-email", fields); err != nil {
		return fmt.Errorf("error loading email template: %v", err)
	}

	auth := smtp.PlainAuth("", es.fromAddress, es.passwd, es.hostname)
	return smtp.SendMail(es.hostname+":"+es.port, auth, es.fromAddress, to, msg.Bytes())

}
