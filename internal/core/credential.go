package core

type Credential struct {
	APIkey       string
	APIProvider  Provider
	CustomHeader string
}
