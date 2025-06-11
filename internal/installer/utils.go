package installer

import (
	"math/rand" // Package rand implements pseudo-random number generators
	"time"      // Package time provides functionality for measuring and displaying time
)

// rnd is a package-level variable holding a pseudo-random number generator (PRNG) instance.
// This is initialized once with a seed based on the current time in nanoseconds,
// which helps ensure that the generated random sequences differ between program runs.
var rnd *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

// randomString generates a random alphanumeric string of specified length n.
// The generated string includes uppercase letters, lowercase letters, and digits.
// This function can be used for generating random IDs, tokens, or temporary values.
func randomString(n int) string {
	// Define the set of characters to choose from:
	// lowercase letters, uppercase letters, and digits 0-9.
	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	// Create a slice of runes with length n to hold the randomly chosen characters.
	b := make([]rune, n)

	// Loop over each index of the slice and assign a random character from 'letters'.
	for i := range b {
		// rnd.Intn(len(letters)) returns a random index within the letters slice.
		b[i] = letters[rnd.Intn(len(letters))]
	}

	// Convert the slice of runes back to a string and return it.
	return string(b)
}
