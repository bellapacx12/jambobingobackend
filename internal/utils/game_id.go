package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)


const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateGameID creates a random 8-character alphanumeric game ID with BB prefix
func GenerateGameID() string {
	var sb strings.Builder
	sb.WriteString("BB")
	for i := 0; i < 6; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		sb.WriteByte(alphabet[n.Int64()])
	}
	return sb.String()
}

// GenerateCartelaMatrix creates a random 5x5 bingo matrix
// B: 1-15, I: 16-30, N: 31-45 (center is free), G: 46-60, O: 61-75
func GenerateCartelaMatrix() [5][5]int {
	var matrix [5][5]int
	used := make(map[int]bool)

	ranges := [5][2]int{
		{1, 15},   // B
		{16, 30},  // I
		{31, 45},  // N
		{46, 60},  // G
		{61, 75},  // O
	}

	for col := 0; col < 5; col++ {
		min, max := ranges[col][0], ranges[col][1]
		for row := 0; row < 5; row++ {
			if col == 2 && row == 2 {
				matrix[row][col] = 0 // Free space
				continue
			}
			for {
				n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
				val := int(n.Int64()) + min
				if !used[val] {
					used[val] = true
					matrix[row][col] = val
					break
				}
			}
		}
	}

	return matrix
}

// CheckBingoWin verifies if the given matrix has a complete line
func CheckBingoWin(matrix [5][5]int, calledNumbers map[int]bool) (bool, string) {
	// Check rows
	for row := 0; row < 5; row++ {
		complete := true
		for col := 0; col < 5; col++ {
			if matrix[row][col] != 0 && !calledNumbers[matrix[row][col]] {
				complete = false
				break
			}
		}
		if complete {
			return true, "row"
		}
	}

	// Check columns
	for col := 0; col < 5; col++ {
		complete := true
		for row := 0; row < 5; row++ {
			if matrix[row][col] != 0 && !calledNumbers[matrix[row][col]] {
				complete = false
				break
			}
		}
		if complete {
			return true, "column"
		}
	}

	// Check diagonals
	diag1Complete := true
	diag2Complete := true
	for i := 0; i < 5; i++ {
		if matrix[i][i] != 0 && !calledNumbers[matrix[i][i]] {
			diag1Complete = false
		}
		if matrix[i][4-i] != 0 && !calledNumbers[matrix[i][4-i]] {
			diag2Complete = false
		}
	}
	if diag1Complete {
		return true, "diagonal"
	}
	if diag2Complete {
		return true, "diagonal"
	}

	return false, ""
}

// FormatBall formats a number as BINGO letter prefix (e.g., "B-3", "O-75")
func FormatBall(number int) string {
	switch {
	case number >= 1 && number <= 15:
		return fmt.Sprintf("B-%d", number)
	case number >= 16 && number <= 30:
		return fmt.Sprintf("I-%d", number)
	case number >= 31 && number <= 45:
		return fmt.Sprintf("N-%d", number)
	case number >= 46 && number <= 60:
		return fmt.Sprintf("G-%d", number)
	case number >= 61 && number <= 75:
		return fmt.Sprintf("O-%d", number)
	default:
		return fmt.Sprintf("?-%d", number)
	}
}

