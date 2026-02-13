package network

func Supported(network string) bool {
	return network == "mainnet" || network == "calibnet"
}
