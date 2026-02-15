package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error,omitempty"`
}

func main() {
	clientID := "Iv1.b507a3d2051307b4" // GitHub Copilot CLI Client ID

	// 1. Request Device Code
	data := fmt.Sprintf("client_id=%s&scope=read:user", clientID)
	req, _ := http.NewRequest("POST", "https://github.com/login/device/code", bytes.NewBufferString(data))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("Error requesting device code: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var dcr DeviceCodeResponse
	json.NewDecoder(resp.Body).Decode(&dcr)

	fmt.Println("-------------------------------------------")
	fmt.Printf("1. Buka URL: %s\n", dcr.VerificationURI)
	fmt.Printf("2. Masukkan Kode: %s\n", dcr.UserCode)
	fmt.Println("-------------------------------------------")
	fmt.Println("Menunggu konfirmasi dari GitHub...")

	// 2. Poll for Token
	ticker := time.NewTicker(time.Duration(dcr.Interval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		pollData := fmt.Sprintf("client_id=%s&device_code=%s&grant_type=urn:ietf:params:oauth:grant-type:device_code", clientID, dcr.DeviceCode)
		preq, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", bytes.NewBufferString(pollData))
		preq.Header.Set("Accept", "application/json")
		preq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		presp, err := http.DefaultClient.Do(preq)
		if err != nil {
			continue
		}
		
		var tr TokenResponse
		json.NewDecoder(presp.Body).Decode(&tr)
		presp.Body.Close()

		if tr.AccessToken != "" {
			fmt.Println("\n✅ Login Berhasil!")
			fmt.Printf("Token kamu: %s\n", tr.AccessToken)
			fmt.Println("\nSilakan copy token di atas ke Dashboard Laravel Apipod (Provider Type: Copilot Native)")
			break
		}

		if tr.Error != "authorization_pending" {
			if tr.Error != "" {
				fmt.Printf("\nError: %s\n", tr.Error)
			}
			break
		}
		fmt.Print(".")
	}
}
