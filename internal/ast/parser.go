package ast

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ParsedFlags captures all CLI flags recognized by braid after separation from
// positional tokens.
type ParsedFlags struct {
	Work          string
	Review        string
	Gate          string
	Iterate       string
	MaxIterations *int // nil if not provided

	Model   string
	Agent   string
	Sandbox string // "agent" | "docker" | ""

	WorkAgent    string
	ReviewAgent  string
	GateAgent    string
	IterateAgent string
	RalphAgent   string

	WorkModel    string
	ReviewModel  string
	GateModel    string
	IterateModel string
	RalphModel   string

	ShowRequest bool // true unless --hide-request
	NoWait      bool
	Yes         bool
	Help        bool
}

// reservedKeywords is the set of tokens that cannot appear as a bare work prompt.
var reservedKeywords = map[string]struct{}{
	"review":  {},
	"ralph":   {},
	"race":    {},
	"repeat":  {},
	"vs":      {},
	"pick":    {},
	"merge":   {},
	"compare": {},
}

var (
	xnPattern    = regexp.MustCompile(`^(?i)x(\d+)$`)
	vnPattern    = regexp.MustCompile(`^(?i)v(\d+)$`)
	bareNumberRe = regexp.MustCompile(`^\d+$`)
)

// valueFlags require a value — either "--flag=value" or "--flag value".
var valueFlags = map[string]struct{}{
	"--work":           {},
	"--review":         {},
	"--gate":           {},
	"--iterate":        {},
	"--model":          {},
	"--agent":          {},
	"--sandbox":        {},
	"--work-agent":     {},
	"--review-agent":   {},
	"--gate-agent":     {},
	"--iterate-agent":  {},
	"--ralph-agent":    {},
	"--work-model":     {},
	"--review-model":   {},
	"--gate-model":     {},
	"--iterate-model":  {},
	"--ralph-model":    {},
	"--max-iterations": {},
}

// booleanFlags are bare flags without values.
var booleanFlags = map[string]struct{}{
	"--hide-request": {},
	"--no-wait":      {},
	"--yes":          {},
}

func isReserved(token string) bool {
	if _, ok := reservedKeywords[strings.ToLower(token)]; ok {
		return true
	}
	return xnPattern.MatchString(token) || vnPattern.MatchString(token)
}

func isBareNumber(token string) bool {
	return bareNumberRe.MatchString(token)
}

// SeparateFlags splits a raw argv slice into named flags (values as strings)
// and positional tokens, preserving relative order of positional tokens.
func SeparateFlags(args []string) (flags map[string]string, positional []string) {
	flags = map[string]string{}
	positional = []string{}

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch arg {
		case "-y":
			flags["--yes"] = "true"
			continue
		case "-h":
			flags["--help"] = "true"
			continue
		}

		if strings.HasPrefix(arg, "--") {
			if idx := strings.Index(arg, "="); idx >= 0 {
				key := arg[:idx]
				val := arg[idx+1:]
				flags[key] = val
				continue
			}
			if _, ok := valueFlags[arg]; ok {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
					flags[arg] = args[i+1]
					i++
				}
				continue
			}
			if _, ok := booleanFlags[arg]; ok {
				flags[arg] = "true"
				continue
			}
			// Unknown flag — ignore silently.
			continue
		}

		positional = append(positional, arg)
	}

	return flags, positional
}

// BuildParsedFlags converts a raw flag map into a typed ParsedFlags struct.
func BuildParsedFlags(flags map[string]string) ParsedFlags {
	out := ParsedFlags{
		Work:         flags["--work"],
		Review:       flags["--review"],
		Gate:         flags["--gate"],
		Iterate:      flags["--iterate"],
		Model:        flags["--model"],
		Agent:        flags["--agent"],
		WorkAgent:    flags["--work-agent"],
		ReviewAgent:  flags["--review-agent"],
		GateAgent:    flags["--gate-agent"],
		IterateAgent: flags["--iterate-agent"],
		RalphAgent:   flags["--ralph-agent"],
		WorkModel:    flags["--work-model"],
		ReviewModel:  flags["--review-model"],
		GateModel:    flags["--gate-model"],
		IterateModel: flags["--iterate-model"],
		RalphModel:   flags["--ralph-model"],
		ShowRequest:  flags["--hide-request"] != "true",
		NoWait:       flags["--no-wait"] == "true",
		Yes:          flags["--yes"] == "true",
		Help:         flags["--help"] == "true",
	}

	if sb := flags["--sandbox"]; sb == "agent" || sb == "docker" {
		out.Sandbox = sb
	}

	if mi, ok := flags["--max-iterations"]; ok {
		if n, err := strconv.Atoi(mi); err == nil {
			out.MaxIterations = &n
		}
	}

	return out
}

// Parse converts a raw argv slice into a parsed AST plus flags.
func Parse(args []string) (Node, ParsedFlags, error) {
	flags, positional := SeparateFlags(args)
	parsedFlags := BuildParsedFlags(flags)

	if len(positional) == 0 {
		if parsedFlags.Work != "" {
			var node Node = &WorkNode{Prompt: parsedFlags.Work}
			if parsedFlags.Review != "" || parsedFlags.Gate != "" {
				maxIter := 3
				if parsedFlags.MaxIterations != nil {
					maxIter = *parsedFlags.MaxIterations
				}
				node = &ReviewNode{
					Inner:         node,
					ReviewPrompt:  parsedFlags.Review,
					GatePrompt:    parsedFlags.Gate,
					IteratePrompt: parsedFlags.Iterate,
					MaxIterations: maxIter,
				}
			}
			return node, parsedFlags, nil
		}
		return nil, parsedFlags, fmt.Errorf("work prompt is required")
	}

	// Check for "vs" keyword splitting branches.
	hasVs := false
	for _, t := range positional {
		if strings.EqualFold(t, "vs") {
			hasVs = true
			break
		}
	}

	if hasVs {
		node, err := parseVsComposition(positional, parsedFlags)
		if err != nil {
			return nil, parsedFlags, err
		}
		return node, parsedFlags, nil
	}

	node, err := parsePipeline(positional, parsedFlags)
	if err != nil {
		return nil, parsedFlags, err
	}
	return node, parsedFlags, nil
}

// parsePipeline converts a non-"vs" token sequence into a nested AST.
func parsePipeline(tokens []string, flags ParsedFlags) (Node, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("work prompt is required")
	}

	if isReserved(tokens[0]) || isBareNumber(tokens[0]) {
		return nil, fmt.Errorf("work prompt is required (got reserved keyword %q)", tokens[0])
	}

	workPrompt := tokens[0]
	if flags.Work != "" {
		workPrompt = flags.Work
	}
	var current Node = &WorkNode{Prompt: workPrompt}
	i := 1

	// Implicit review: bare strings/numbers before any keyword fill review→gate→iterate slots.
	if i < len(tokens) && !isReserved(tokens[i]) {
		positionalPrompts := []string{}
		var implicitMaxIterations *int

		for i < len(tokens) && !isReserved(tokens[i]) {
			if isBareNumber(tokens[i]) {
				n, _ := strconv.Atoi(tokens[i])
				implicitMaxIterations = &n
				i++
				break
			}
			positionalPrompts = append(positionalPrompts, tokens[i])
			i++
		}

		if len(positionalPrompts) > 0 || implicitMaxIterations != nil {
			if len(positionalPrompts) > 0 {
				review := firstNonEmpty(flags.Review, positionalPrompts, 0)
				gate := firstNonEmpty(flags.Gate, positionalPrompts, 1)
				iterate := firstNonEmpty(flags.Iterate, positionalPrompts, 2)
				maxIter := 3
				if implicitMaxIterations != nil {
					maxIter = *implicitMaxIterations
				} else if flags.MaxIterations != nil {
					maxIter = *flags.MaxIterations
				}
				current = &ReviewNode{
					Inner:         current,
					ReviewPrompt:  review,
					GatePrompt:    gate,
					IteratePrompt: iterate,
					MaxIterations: maxIter,
				}
			} else {
				maxIter := *implicitMaxIterations
				current = &ReviewNode{
					Inner:         current,
					ReviewPrompt:  flags.Review,
					GatePrompt:    flags.Gate,
					IteratePrompt: flags.Iterate,
					MaxIterations: maxIter,
				}
			}
		}
	}

	// Main keyword scan.
	for i < len(tokens) {
		token := tokens[i]
		lower := strings.ToLower(token)

		// xN
		if m := xnPattern.FindStringSubmatch(token); m != nil {
			count, _ := strconv.Atoi(m[1])
			if count > 1 {
				current = &RepeatNode{Inner: current, Count: count}
			}
			i++
			continue
		}

		// repeat N
		if lower == "repeat" {
			i++
			if i >= len(tokens) || !isBareNumber(tokens[i]) {
				return nil, fmt.Errorf("repeat requires a number (e.g., repeat 3)")
			}
			count, _ := strconv.Atoi(tokens[i])
			if count > 1 {
				current = &RepeatNode{Inner: current, Count: count}
			}
			i++
			continue
		}

		// review [prompts...] [N]
		if lower == "review" {
			i++
			reviewPrompt := flags.Review
			gatePrompt := flags.Gate
			iteratePrompt := flags.Iterate
			maxIter := 3
			if flags.MaxIterations != nil {
				maxIter = *flags.MaxIterations
			}

			prompts := []string{}
			for i < len(tokens) && !isReserved(tokens[i]) {
				if isBareNumber(tokens[i]) {
					maxIter, _ = strconv.Atoi(tokens[i])
					i++
					break
				}
				prompts = append(prompts, tokens[i])
				i++
			}

			if len(prompts) >= 1 && reviewPrompt == "" {
				reviewPrompt = prompts[0]
			}
			if len(prompts) >= 2 && gatePrompt == "" {
				gatePrompt = prompts[1]
			}
			if len(prompts) >= 3 && iteratePrompt == "" {
				iteratePrompt = prompts[2]
			}

			current = &ReviewNode{
				Inner:         current,
				ReviewPrompt:  reviewPrompt,
				GatePrompt:    gatePrompt,
				IteratePrompt: iteratePrompt,
				MaxIterations: maxIter,
			}
			continue
		}

		// ralph [N] "gate prompt"
		if lower == "ralph" {
			i++
			maxTasks := 100

			if i < len(tokens) && isBareNumber(tokens[i]) {
				maxTasks, _ = strconv.Atoi(tokens[i])
				i++
			}

			if i >= len(tokens) || isReserved(tokens[i]) || isBareNumber(tokens[i]) {
				return nil, fmt.Errorf("ralph requires a gate prompt string")
			}
			gatePrompt := tokens[i]
			i++

			current = &RalphNode{Inner: current, MaxTasks: maxTasks, GatePrompt: gatePrompt}
			continue
		}

		// vN or race N — composition
		if vm := vnPattern.FindStringSubmatch(token); vm != nil || lower == "race" {
			var count int
			if vm != nil {
				count, _ = strconv.Atoi(vm[1])
				i++
			} else {
				i++
				if i >= len(tokens) || !isBareNumber(tokens[i]) {
					return nil, fmt.Errorf("race requires a number (e.g., race 3)")
				}
				count, _ = strconv.Atoi(tokens[i])
				i++
			}

			if count <= 1 {
				continue // v1 / race 1 is a no-op
			}

			branches := make([]Node, count)
			for b := 0; b < count; b++ {
				branches[b] = cloneNode(current)
			}

			resolver, criteria, consumed := parseResolver(tokens[i:])
			i += consumed
			current = &CompositionNode{Branches: branches, Resolver: resolver, Criteria: criteria}

			// Second-level composition (compare cannot be followed by one)
			if resolver != ResolverCompare && i < len(tokens) {
				next := tokens[i]
				if vm2 := vnPattern.FindStringSubmatch(next); vm2 != nil || strings.ToLower(next) == "race" {
					var count2 int
					if vm2 != nil {
						count2, _ = strconv.Atoi(vm2[1])
						i++
					} else {
						i++
						if i >= len(tokens) || !isBareNumber(tokens[i]) {
							return nil, fmt.Errorf("race requires a number (e.g., race 3)")
						}
						count2, _ = strconv.Atoi(tokens[i])
						i++
					}

					if count2 > 1 {
						secondBranches := make([]Node, count2)
						for b := 0; b < count2; b++ {
							secondBranches[b] = cloneNode(current)
						}
						r2, c2, used := parseResolver(tokens[i:])
						i += used
						current = &CompositionNode{Branches: secondBranches, Resolver: r2, Criteria: c2}
					}
				}
			}
			continue
		}

		// Bare pick/merge/compare (without preceding vN) — skip, reached only in malformed input.
		if lower == "pick" || lower == "merge" || lower == "compare" {
			i++
			continue
		}

		return nil, fmt.Errorf("unknown token %q. expected a keyword (review, ralph, repeat, race, vs, pick, merge, compare) or a pattern like x3, v3", token)
	}

	return current, nil
}

// parseResolver reads a resolver keyword and optional criteria from the start
// of tokens, returning the parsed resolver, criteria, and number of tokens consumed.
func parseResolver(tokens []string) (Resolver, string, int) {
	if len(tokens) == 0 {
		return ResolverPick, "", 0
	}

	first := strings.ToLower(tokens[0])
	switch first {
	case "pick", "merge", "compare":
		resolver := resolverFromString(first)
		consumed := 1
		// compare never takes criteria
		if resolver != ResolverCompare && len(tokens) > 1 && !isReserved(tokens[1]) && !isBareNumber(tokens[1]) {
			return resolver, tokens[1], 2
		}
		return resolver, "", consumed
	}

	// Implicit pick with bare criteria string.
	if !isReserved(tokens[0]) && !isBareNumber(tokens[0]) {
		return ResolverPick, tokens[0], 1
	}

	return ResolverPick, "", 0
}

func resolverFromString(s string) Resolver {
	switch s {
	case "merge":
		return ResolverMerge
	case "compare":
		return ResolverCompare
	default:
		return ResolverPick
	}
}

// parseVsComposition handles "A vs B [vs C] [resolver criteria] [second-level composition]".
func parseVsComposition(positional []string, flags ParsedFlags) (Node, error) {
	segments := [][]string{}
	current := []string{}
	resolverTokens := []string{}
	pastLastBranch := false

	for _, token := range positional {
		lower := strings.ToLower(token)

		if lower == "vs" && !pastLastBranch {
			if len(current) == 0 {
				return nil, fmt.Errorf(`empty branch before "vs"`)
			}
			segments = append(segments, current)
			current = []string{}
			continue
		}

		if !pastLastBranch {
			// Resolver keyword after at least one branch ends the branch scan.
			if len(segments) > 0 && (lower == "pick" || lower == "merge" || lower == "compare") {
				if len(current) > 0 {
					segments = append(segments, current)
					current = []string{}
				}
				pastLastBranch = true
				resolverTokens = append(resolverTokens, token)
				continue
			}
			current = append(current, token)
		} else {
			resolverTokens = append(resolverTokens, token)
		}
	}

	if !pastLastBranch && len(current) > 0 {
		segments = append(segments, current)
	}

	if len(segments) < 2 {
		return nil, fmt.Errorf("vs requires at least 2 branches")
	}

	branches := make([]Node, len(segments))
	for idx, seg := range segments {
		node, err := parsePipeline(seg, flags)
		if err != nil {
			return nil, err
		}
		branches[idx] = node
	}

	resolver, criteria, consumed := parseResolver(resolverTokens)
	rIdx := consumed

	var node Node = &CompositionNode{Branches: branches, Resolver: resolver, Criteria: criteria}

	// Second-level composition after the first resolver.
	if resolver != ResolverCompare && rIdx < len(resolverTokens) {
		next := resolverTokens[rIdx]
		if vm := vnPattern.FindStringSubmatch(next); vm != nil || strings.ToLower(next) == "race" {
			var count2 int
			if vm != nil {
				count2, _ = strconv.Atoi(vm[1])
				rIdx++
			} else {
				rIdx++
				if rIdx >= len(resolverTokens) || !isBareNumber(resolverTokens[rIdx]) {
					return nil, fmt.Errorf("race requires a number")
				}
				count2, _ = strconv.Atoi(resolverTokens[rIdx])
				rIdx++
			}

			if count2 > 1 {
				secondBranches := make([]Node, count2)
				for b := 0; b < count2; b++ {
					secondBranches[b] = cloneNode(node)
				}
				r2, c2, used := parseResolver(resolverTokens[rIdx:])
				rIdx += used
				node = &CompositionNode{Branches: secondBranches, Resolver: r2, Criteria: c2}
			}
		}
	}

	return node, nil
}

// firstNonEmpty returns explicit (from flag) if set, else prompts[idx] if in range.
func firstNonEmpty(explicit string, prompts []string, idx int) string {
	if explicit != "" {
		return explicit
	}
	if idx < len(prompts) {
		return prompts[idx]
	}
	return ""
}

// cloneNode performs a deep copy of an AST node, used to create independent
// branches for compositions.
func cloneNode(n Node) Node {
	switch v := n.(type) {
	case *WorkNode:
		cp := *v
		return &cp
	case *RepeatNode:
		cp := *v
		cp.Inner = cloneNode(v.Inner)
		return &cp
	case *ReviewNode:
		cp := *v
		cp.Inner = cloneNode(v.Inner)
		return &cp
	case *RalphNode:
		cp := *v
		cp.Inner = cloneNode(v.Inner)
		return &cp
	case *CompositionNode:
		cp := *v
		cp.Branches = make([]Node, len(v.Branches))
		for i, b := range v.Branches {
			cp.Branches[i] = cloneNode(b)
		}
		return &cp
	}
	return n
}
