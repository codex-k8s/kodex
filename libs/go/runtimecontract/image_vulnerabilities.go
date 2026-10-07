package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ImageVulnerabilityReportSchema = "kodex.dev/image-vulnerability-report/v1"
const MaximumImageVulnerabilityReportBytes = 4 << 20
const MaximumImageVulnerabilityFindings = 10000
const MaximumImageVulnerabilitySourceBytes = 64 << 20
const MaximumImageVulnerabilityProjectionInputBytes = ((MaximumImageVulnerabilitySourceBytes + 2) / 3 * 4) + MaximumImageVulnerabilityReportBytes
const maximumImageVulnerabilityJSONDepth = 32

var errImageVulnerabilities = errors.New("image vulnerability report is invalid")
var vulnerabilityIdentifier = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9.+_:@/~-]{0,319}$`)
var vulnerabilityVersion = regexp.MustCompile(`^[A-Za-z0-9<>=~^*][A-Za-z0-9.+:~_|<>=,^* -]{0,159}$`)
var vulnerabilityAdvisory = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,159}$`)
var vulnerabilityCVE = regexp.MustCompile(`^CVE-[0-9]{4}-[0-9]{4,12}$`)
var vulnerabilityGHSA = regexp.MustCompile(`^GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}$`)
var vulnerabilityGO = regexp.MustCompile(`^GO-[0-9]{4}-[0-9]{4,12}$`)

var vulnerabilitySeverities = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "NEGLIGIBLE", "UNKNOWN"}

// Проекция не содержит scanner URLs, paths, headers, raw metadata или credentials.
// Все находки сохранены; grouping не меняет количества исходных совпадений.
type ImageVulnerabilityFinding struct {
	Ref              string   `json:"ref"`
	PackageName      string   `json:"packageName"`
	InstalledVersion string   `json:"installedVersion"`
	Ecosystem        string   `json:"ecosystem"`
	AdvisoryID       string   `json:"advisoryId"`
	AdvisoryKind     string   `json:"advisoryKind"`
	AdvisoryURL      string   `json:"advisoryUrl"`
	Severity         string   `json:"severity"`
	FixState         string   `json:"fixState"`
	FixedVersions    []string `json:"fixedVersions"`
	Blocking         bool     `json:"blocking"`
	Ignored          bool     `json:"ignored"`
	Occurrences      uint32   `json:"occurrences"`
}

type ImageVulnerabilitySeverityCount struct {
	Severity   string `json:"severity"`
	MatchCount uint32 `json:"matchCount"`
}

// Tuple связывает полный отчёт с immutable build. Lifecycle/version/receipt
// добавляет владелец вне проекции, исключая циклический digest.
type ImageVulnerabilityReport struct {
	Schema                    string                            `json:"schema"`
	ArtifactRef               string                            `json:"artifactRef"`
	ImageDigest               string                            `json:"imageDigest"`
	ReportSHA256              string                            `json:"reportSHA256"`
	SBOMSHA256                string                            `json:"sbomSHA256"`
	ScopeKind                 string                            `json:"scopeKind"`
	OrganizationRef           string                            `json:"organizationRef"`
	ProjectRef                string                            `json:"projectRef"`
	RecipeRef                 string                            `json:"recipeRef"`
	RecipeVersion             uint64                            `json:"recipeVersion"`
	RecipeGeneration          uint64                            `json:"recipeGeneration"`
	BuildRef                  string                            `json:"buildRef"`
	BuildVersion              uint64                            `json:"buildVersion"`
	BuildAttempt              uint32                            `json:"buildAttempt"`
	PolicyRevision            uint64                            `json:"policyRevision"`
	PolicySHA256              string                            `json:"policySHA256"`
	MatchCount                uint32                            `json:"matchCount"`
	UniqueAdvisoryCount       uint32                            `json:"uniqueAdvisoryCount"`
	BlockingMatchCount        uint32                            `json:"blockingMatchCount"`
	UnresolvedNoFixMatchCount uint32                            `json:"unresolvedNoFixMatchCount"`
	SuppressedMatchCount      uint32                            `json:"suppressedMatchCount"`
	SeverityCounts            []ImageVulnerabilitySeverityCount `json:"severityCounts"`
	Findings                  []ImageVulnerabilityFinding       `json:"findings"`
}

func vulnerabilitySafeText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

func vulnerabilityRef(value, prefix string) bool {
	return strings.HasPrefix(value, prefix+"_") && opaqueReferencePattern.MatchString(value)
}

func vulnerabilityPin(value uint64) bool { return value > 0 && value <= 9007199254740991 }

func vulnerabilityScope(kind, organization, project string) bool {
	return vulnerabilityRef(organization, "org") &&
		(kind == "ORGANIZATION" && project == "" || kind == "PROJECT" && vulnerabilityRef(project, "prj"))
}

func vulnerabilityAdvisoryLink(id string) (string, string) {
	switch {
	case vulnerabilityCVE.MatchString(id):
		return "CVE", "https://nvd.nist.gov/vuln/detail/" + id
	case vulnerabilityGHSA.MatchString(id):
		return "GHSA", "https://github.com/advisories/" + id
	case vulnerabilityGO.MatchString(id):
		return "GO", "https://pkg.go.dev/vuln/" + id
	default:
		return "OTHER", ""
	}
}

func ImageVulnerabilitySHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// Identity считается по полному canonical tuple без количества повторений.
func imageVulnerabilityFindingRef(value ImageVulnerabilityFinding) string {
	value.Ref, value.Occurrences = "", 0
	raw, _ := json.Marshal(value)
	return ImageVulnerabilitySHA256(raw)
}

func (value ImageVulnerabilityReport) Validate() error {
	if value.Schema != ImageVulnerabilityReportSchema || !vulnerabilityRef(value.ArtifactRef, "imgart") ||
		!imageDigestPattern.MatchString(value.ImageDigest) || !lowerHexDigestPattern.MatchString(value.ReportSHA256) ||
		!lowerHexDigestPattern.MatchString(value.SBOMSHA256) || !vulnerabilityScope(value.ScopeKind, value.OrganizationRef, value.ProjectRef) ||
		!vulnerabilityRef(value.RecipeRef, "imgrec") || !vulnerabilityRef(value.BuildRef, "imgbld") ||
		!vulnerabilityPin(value.RecipeVersion) || !vulnerabilityPin(value.RecipeGeneration) || !vulnerabilityPin(value.BuildVersion) ||
		value.BuildAttempt == 0 || !vulnerabilityPin(value.PolicyRevision) || !lowerHexDigestPattern.MatchString(value.PolicySHA256) ||
		value.Findings == nil || len(value.Findings) > MaximumImageVulnerabilityFindings || len(value.SeverityCounts) != len(vulnerabilitySeverities) {
		return errImageVulnerabilities
	}
	var total, blocking, noFix, ignored uint64
	counts, advisories := map[string]uint64{}, map[string]bool{}
	previous := ""
	for _, finding := range value.Findings {
		kind, link := vulnerabilityAdvisoryLink(finding.AdvisoryID)
		high := finding.Severity == "CRITICAL" || finding.Severity == "HIGH"
		fixed := finding.FixState == "FIXED" && len(finding.FixedVersions) > 0
		if !vulnerabilityIdentifier.MatchString(finding.PackageName) || !vulnerabilitySafeText(finding.InstalledVersion, 160) ||
			!vulnerabilityVersion.MatchString(finding.InstalledVersion) || !vulnerabilityIdentifier.MatchString(finding.Ecosystem) ||
			!vulnerabilityAdvisory.MatchString(finding.AdvisoryID) || finding.AdvisoryKind != kind || finding.AdvisoryURL != link ||
			!slices.Contains(vulnerabilitySeverities, finding.Severity) ||
			!slices.Contains([]string{"FIXED", "NOT_FIXED", "WONT_FIX", "UNKNOWN"}, finding.FixState) ||
			finding.FixedVersions == nil || len(finding.FixedVersions) > 64 || finding.FixState == "FIXED" && !fixed ||
			finding.Blocking != (!finding.Ignored && high && fixed) || finding.Occurrences == 0 ||
			finding.Ref != imageVulnerabilityFindingRef(finding) || finding.Ref <= previous {
			return errImageVulnerabilities
		}
		previous = finding.Ref
		for index, version := range finding.FixedVersions {
			if !vulnerabilityVersion.MatchString(version) || index > 0 && version <= finding.FixedVersions[index-1] {
				return errImageVulnerabilities
			}
		}
		occurrences := uint64(finding.Occurrences)
		total += occurrences
		counts[finding.Severity] += occurrences
		advisories[finding.AdvisoryID] = true
		if finding.Blocking {
			blocking += occurrences
		}
		if finding.Ignored {
			ignored += occurrences
		} else if high && !fixed {
			noFix += occurrences
		}
	}
	if total > math.MaxUint32 || total != uint64(value.MatchCount) || blocking != uint64(value.BlockingMatchCount) ||
		noFix != uint64(value.UnresolvedNoFixMatchCount) || ignored != uint64(value.SuppressedMatchCount) ||
		len(advisories) != int(value.UniqueAdvisoryCount) {
		return errImageVulnerabilities
	}
	for index, severity := range vulnerabilitySeverities {
		if value.SeverityCounts[index].Severity != severity || uint64(value.SeverityCounts[index].MatchCount) != counts[severity] {
			return errImageVulnerabilities
		}
	}
	return nil
}

func CanonicalImageVulnerabilityReport(value ImageVulnerabilityReport) ([]byte, error) {
	if value.Validate() != nil {
		return nil, errImageVulnerabilities
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaximumImageVulnerabilityReportBytes {
		return nil, errImageVulnerabilities
	}
	return raw, nil
}

func decodeVulnerabilityJSON(raw []byte, target any, maximum int, canonical bool) error {
	if len(raw) == 0 || len(raw) > maximum || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, maximumImageVulnerabilityJSONDepth) != nil {
		return errImageVulnerabilities
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		return errImageVulnerabilities
	}
	if canonical {
		encoded, _ := json.Marshal(target)
		if !bytes.Equal(raw, encoded) {
			return errImageVulnerabilities
		}
	}
	return nil
}

func DecodeImageVulnerabilityReport(raw []byte) (ImageVulnerabilityReport, error) {
	var value ImageVulnerabilityReport
	if decodeVulnerabilityJSON(raw, &value, MaximumImageVulnerabilityReportBytes, true) != nil || value.Validate() != nil {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	return value, nil
}

// Envelope используется чистым worker-валидатором: claim credentials в него
// не входят. Duplicate keys отклоняются и во внешнем scanner payload.
func ProjectImageVulnerabilityEnvelope(raw []byte) (ImageVulnerabilityReport, error) {
	var envelope struct {
		Binding           ImageVulnerabilityReport `json:"binding"`
		ReportBytesBase64 string                   `json:"reportBytesBase64"`
	}
	if decodeVulnerabilityJSON(raw, &envelope, MaximumImageVulnerabilityProjectionInputBytes, false) != nil ||
		len(envelope.ReportBytesBase64) > base64.StdEncoding.EncodedLen(MaximumImageVulnerabilitySourceBytes) {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	// JSON-вложение меняет whitespace и hash. Base64 сохраняет исходные scanner
	// bytes, включая завершающий перевод строки, до проверки и подписи.
	source, err := base64.StdEncoding.Strict().DecodeString(envelope.ReportBytesBase64)
	if err != nil || len(source) == 0 || len(source) > MaximumImageVulnerabilitySourceBytes ||
		base64.StdEncoding.EncodeToString(source) != envelope.ReportBytesBase64 {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	defer clear(source)
	return ProjectImageVulnerabilityReport(source, envelope.Binding)
}

type grypeProjectionMatch struct {
	Artifact struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Type    string `json:"type"`
	} `json:"artifact"`
	Vulnerability struct {
		ID       string `json:"id"`
		Severity string `json:"severity"`
		Fix      struct {
			State    *string  `json:"state"`
			Versions []string `json:"versions"`
		} `json:"fix"`
	} `json:"vulnerability"`
}

// Raw scanner JSON — внешняя схема: лишние metadata разрешены только здесь и
// никогда не копируются в owner projection. Duplicate JSON всегда отклоняется.
func ProjectImageVulnerabilityReport(raw []byte, binding ImageVulnerabilityReport) (ImageVulnerabilityReport, error) {
	if len(raw) == 0 || len(raw) > MaximumImageVulnerabilitySourceBytes ||
		boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, maximumImageVulnerabilityJSONDepth) != nil {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	var source struct {
		Matches        []grypeProjectionMatch `json:"matches"`
		IgnoredMatches []grypeProjectionMatch `json:"ignoredMatches"`
		Policy         struct {
			Schema         string `json:"schema"`
			PolicyRevision uint64 `json:"policyRevision"`
			PolicySHA256   string `json:"policySHA256"`
			HighOrCritical uint32 `json:"highOrCriticalMatchCount"`
			Blocking       uint32 `json:"blockingMatchCount"`
			NoFix          uint32 `json:"unresolvedNoFixMatchCount"`
		} `json:"kodexPolicy"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(&source) != nil || decoder.Decode(new(any)) != io.EOF || source.Matches == nil ||
		source.Policy.Schema != "kodex.dev/fix-available-high-or-critical/v1" || source.Policy.PolicyRevision != binding.PolicyRevision ||
		source.Policy.PolicySHA256 != binding.PolicySHA256 || uint64(len(source.Matches))+uint64(len(source.IgnoredMatches)) > math.MaxUint32 {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	digest := ImageVulnerabilitySHA256(raw)
	if binding.ReportSHA256 != "" && binding.ReportSHA256 != digest {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	binding.Schema, binding.ReportSHA256 = ImageVulnerabilityReportSchema, digest
	binding.MatchCount, binding.UniqueAdvisoryCount, binding.BlockingMatchCount, binding.UnresolvedNoFixMatchCount, binding.SuppressedMatchCount = 0, 0, 0, 0, 0
	groups, counts, advisories := map[string]ImageVulnerabilityFinding{}, map[string]uint32{}, map[string]bool{}
	var high uint32
	for setIndex, matches := range [][]grypeProjectionMatch{source.Matches, source.IgnoredMatches} {
		for _, match := range matches {
			fixes := append([]string{}, match.Vulnerability.Fix.Versions...)
			slices.Sort(fixes)
			fixes = slices.Compact(fixes)
			state := ""
			if sourceState := match.Vulnerability.Fix.State; sourceState != nil {
				state = map[string]string{"fixed": "FIXED", "not-fixed": "NOT_FIXED", "wont-fix": "WONT_FIX", "unknown": "UNKNOWN"}[*sourceState]
				// Grype может явно сериализовать нулевое состояние без исправлений.
				// Только эта полная внешняя форма означает UNKNOWN: отсутствующие
				// поля, null и противоречащие ей версии остаются закрытым отказом.
				if *sourceState == "" && match.Vulnerability.Fix.Versions != nil && len(match.Vulnerability.Fix.Versions) == 0 {
					state = "UNKNOWN"
				}
			}
			severity := strings.ToUpper(match.Vulnerability.Severity)
			kind, link := vulnerabilityAdvisoryLink(match.Vulnerability.ID)
			finding := ImageVulnerabilityFinding{PackageName: match.Artifact.Name, InstalledVersion: match.Artifact.Version,
				Ecosystem: match.Artifact.Type, AdvisoryID: match.Vulnerability.ID, AdvisoryKind: kind, AdvisoryURL: link,
				Severity: severity, FixState: state, FixedVersions: fixes, Ignored: setIndex == 1}
			isHigh := severity == "HIGH" || severity == "CRITICAL"
			finding.Blocking = !finding.Ignored && isHigh && state == "FIXED" && len(fixes) > 0
			finding.Ref = imageVulnerabilityFindingRef(finding)
			group := groups[finding.Ref]
			finding.Occurrences = group.Occurrences + 1
			groups[finding.Ref] = finding
			if len(groups) > MaximumImageVulnerabilityFindings {
				return ImageVulnerabilityReport{}, errImageVulnerabilities
			}
			binding.MatchCount++
			counts[severity]++
			advisories[finding.AdvisoryID] = true
			if finding.Ignored {
				binding.SuppressedMatchCount++
			} else if isHigh {
				high++
				if finding.Blocking {
					binding.BlockingMatchCount++
				} else {
					binding.UnresolvedNoFixMatchCount++
				}
			}
		}
	}
	if high != source.Policy.HighOrCritical || binding.BlockingMatchCount != source.Policy.Blocking ||
		binding.UnresolvedNoFixMatchCount != source.Policy.NoFix {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	binding.UniqueAdvisoryCount = uint32(len(advisories))
	binding.Findings = make([]ImageVulnerabilityFinding, 0, len(groups))
	for _, finding := range groups {
		binding.Findings = append(binding.Findings, finding)
	}
	slices.SortFunc(binding.Findings, func(a, b ImageVulnerabilityFinding) int { return strings.Compare(a.Ref, b.Ref) })
	binding.SeverityCounts = make([]ImageVulnerabilitySeverityCount, 0, len(vulnerabilitySeverities))
	for _, severity := range vulnerabilitySeverities {
		binding.SeverityCounts = append(binding.SeverityCounts, ImageVulnerabilitySeverityCount{severity, counts[severity]})
	}
	if _, err := CanonicalImageVulnerabilityReport(binding); err != nil {
		return ImageVulnerabilityReport{}, errImageVulnerabilities
	}
	return binding, nil
}
