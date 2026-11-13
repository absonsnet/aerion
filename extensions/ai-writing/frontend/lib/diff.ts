// Word-level LCS diff — port of diffWords / tokenize / mergeTokens from
// the Thunderbird add-on's popup.js (absonsnet/AI-Writing-Assistant-for-
// Thunderbird v0.5.16, Apache 2.0; keep NOTICE attribution when porting).
//
// Same algorithm: full LCS table + backtrack, then merge consecutive
// tokens of the same type. Two deliberate changes from the original:
//  - Typed (DiffToken) for Svelte consumption.
//  - The original declares two unused `prev`/`curr` rows ("Optimized: use
//    two rows instead of full matrix") and then builds the full `dp` table
//    anyway for backtracking; the dead rows are dropped here.

export type DiffTokenType = 'equal' | 'insert' | 'delete'

export interface DiffToken {
  type: DiffTokenType
  value: string
}

export function diffWords(oldStr: string, newStr: string): DiffToken[] {
  const oldWords = tokenize(oldStr)
  const newWords = tokenize(newStr)

  // Build LCS table (the full table is needed for backtracking).
  const m = oldWords.length
  const n = newWords.length

  const dp: number[][] = []
  for (let i = 0; i <= m; i++) {
    dp[i] = new Array(n + 1).fill(0)
  }

  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      if (oldWords[i - 1] === newWords[j - 1]) {
        dp[i][j] = dp[i - 1][j - 1] + 1
      } else {
        dp[i][j] = Math.max(dp[i - 1][j], dp[i][j - 1])
      }
    }
  }

  // Backtrack to build diff
  const result: DiffToken[] = []
  let i = m
  let j = n

  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldWords[i - 1] === newWords[j - 1]) {
      result.unshift({ type: 'equal', value: oldWords[i - 1] })
      i--
      j--
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      result.unshift({ type: 'insert', value: newWords[j - 1] })
      j--
    } else {
      result.unshift({ type: 'delete', value: oldWords[i - 1] })
      i--
    }
  }

  // Merge consecutive tokens of the same type
  return mergeTokens(result)
}

export function tokenize(str: string): string[] {
  // Split into words and whitespace, keeping whitespace as separate tokens
  return str.match(/\S+|\s+/g) || []
}

export function mergeTokens(tokens: DiffToken[]): DiffToken[] {
  if (!tokens.length) return tokens

  const merged: DiffToken[] = [{ ...tokens[0] }]
  for (let i = 1; i < tokens.length; i++) {
    const last = merged[merged.length - 1]
    if (tokens[i].type === last.type) {
      last.value += tokens[i].value
    } else {
      merged.push({ ...tokens[i] })
    }
  }
  return merged
}
