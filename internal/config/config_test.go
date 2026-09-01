package config

import (
	"strings"
	"testing"
	"time"
)

const base = `
seed: 7
metrics: { listen: ":9090" }
defaults:
  window_size: 10
  submit_resp_latency: { dist: fixed, mean: 20ms }
  throttle: { tps: 100 }
  dlr:
    delay: { min: 1s, max: 5s }
    outcomes: { DELIVRD: 90, UNDELIV: 10 }
operators:
  - name: beeline
    listen: "127.0.0.1:2775"
    accounts: [{ system_id: bee, password: pw }]
  - name: ucell
    listen: "127.0.0.1:2776"
    accounts: [{ system_id: ucl, password: pw }]
    throttle: { tps: 50 }
    dlr:
      outcomes: { DELIVRD: 80, UNDELIV: 20 }
      err_codes: { UNDELIV: 1282 }
`

func TestParseInheritsDefaults(t *testing.T) {
	cfg, err := Parse([]byte(base))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Operators) != 2 {
		t.Fatalf("operators = %d", len(cfg.Operators))
	}
	bee, ucl := cfg.Operators[0], cfg.Operators[1]

	if bee.WindowSize != 10 || ucl.WindowSize != 10 {
		t.Errorf("window_size not inherited: %d %d", bee.WindowSize, ucl.WindowSize)
	}
	if bee.SubmitRespLatency.Mean.D() != 20*time.Millisecond {
		t.Errorf("latency not inherited: %v", bee.SubmitRespLatency.Mean.D())
	}
	if got, _, _ := bee.Throttle.Rate(); got != 100 {
		t.Errorf("beeline tps = %v, want 100 (inherited)", got)
	}
	if got, _, _ := ucl.Throttle.Rate(); got != 50 {
		t.Errorf("ucell tps = %v, want 50 (override)", got)
	}
	// ucell overrides outcomes but inherits delay
	if ucl.DLR.Delay.Max.D() != 5*time.Second {
		t.Errorf("ucell dlr delay not inherited: %v", ucl.DLR.Delay.Max.D())
	}
	if ucl.DLR.Outcomes["UNDELIV"] != 20 {
		t.Errorf("ucell outcomes not overridden: %v", ucl.DLR.Outcomes)
	}
	if ucl.DLR.ErrCodes["UNDELIV"] != 1282 {
		t.Errorf("ucell err_codes = %v", ucl.DLR.ErrCodes)
	}
	// overriding must not mutate the shared defaults / the other operator
	if bee.DLR.Outcomes["DELIVRD"] != 90 {
		t.Errorf("beeline outcomes leaked from ucell override: %v", bee.DLR.Outcomes)
	}
	if bee.DLR.Template != DefaultReceiptTemplate {
		t.Errorf("default template not applied: %q", bee.DLR.Template)
	}
	if bee.SMPPVersion != "3.4" {
		t.Errorf("default smpp_version = %q", bee.SMPPVersion)
	}
}

func TestThrottleRate(t *testing.T) {
	r, b, ok := Throttle{Count: 300, Window: Duration(60 * time.Second)}.Rate()
	if !ok || r != 5 || b != 300 {
		t.Errorf("count/window => rate=%v burst=%v ok=%v, want 5/300", r, b, ok)
	}
	if _, _, ok := (Throttle{}).Rate(); ok {
		t.Error("empty throttle should be unlimited")
	}
}

func TestParseRejectsBadConfig(t *testing.T) {
	cases := map[string]string{
		"no operators":    "operators: []",
		"dup listen":      strings.ReplaceAll(base, "127.0.0.1:2776", "127.0.0.1:2775"),
		"unknown field":   base + "\nbogus: true\n",
		"bad outcome":     strings.ReplaceAll(base, "UNDELIV: 10", "NOPE: 10"),
		"missing account": strings.ReplaceAll(base, "    accounts: [{ system_id: bee, password: pw }]\n", ""),
	}
	for name, src := range cases {
		if _, err := Parse([]byte(src)); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}
