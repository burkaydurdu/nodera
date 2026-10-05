package main

import "errors"

var errNoCertificate = errors.New("the smtp relay needs a certificate outside development: set smtp.cert_file and smtp.key_file")
