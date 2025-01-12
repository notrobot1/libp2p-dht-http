package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	p2phttp "github.com/libp2p/go-libp2p-http"
	"github.com/libp2p/go-libp2p/core/host"
)

type MessageData struct {
	Message string `json:"message"`
	ID      string `json:"id"`
}

// Structure for storing data with signature
type SignedData struct {
	Data      MessageData `json:"data"`
	Signature []byte      `json:"signature"`
}

// Send a GET request to a peer
func sendRequest(hostID string, h host.Host, method string, message string) ([]byte, error) {
	tr := &http.Transport{}
	tr.RegisterProtocol("libp2p", p2phttp.NewTransport(h))
	client := &http.Client{Transport: tr}

	// Example of data to send in JSON format
	data := MessageData{
		Message: message,
		ID:      h.ID().String(),
	}

	
	body, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	privateKey := h.Peerstore().PrivKey(h.ID())

	// Convert the private key to RSA (if necessary)
	rsaPrivateKey, err := privateKey.Sign(body)
	if err != nil {
		return nil, err
	}

	
	// Create a structure with a signature
	signedData := SignedData{
		Data:      data,
		Signature: rsaPrivateKey,
	}

	// Convert data with signature to JSON format
	signedBody, err := json.Marshal(signedData)
	if err != nil {
		return nil, err
	}

	// Forming the request with JSON in the body
	req, err := http.NewRequest(method, "libp2p://"+hostID+"/hello", bytes.NewBuffer(signedBody))
	if err != nil {
		return nil, err
	}

	
	req.Header.Set("Content-Type", "application/json")

	// send the request
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	// read the response
	responseBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	// print the response
	fmt.Println("Response:", string(responseBody))
	return responseBody, nil
}



func auth(r *http.Request) (MessageData, bool, error) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return MessageData{}, false, err
	}

	var signedData SignedData
	err = json.Unmarshal(body, &signedData)
	if err != nil {
		return MessageData{}, false, err
	}

	// Extract data and signature
	data := signedData.Data
	value, found := cache.Get(data.ID)
	if found {

		dataBytes, err := json.Marshal(data)
		if err != nil {
			return MessageData{}, false, err
		}
		valid, err := value.Verify(dataBytes, signedData.Signature)
		if err != nil {
			return MessageData{}, false, err
		}
		return data, valid, nil

	}
	return MessageData{}, false, nil
}
