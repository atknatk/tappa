package apikey

// KeyedHashForTest exposes the stored value's construction to the external tests' canaries.
func KeyedHashForTest(macKey []byte, raw string) string {
	h, err := keyedHash(macKey, raw)
	if err != nil {
		panic(err)
	}
	return h
}
