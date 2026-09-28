package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"billforge/lab"
)

type menu struct {
	in    *bufio.Reader
	out   io.Writer
	lab   *lab.Lab
	fixed *time.Time
}

type menuOption struct {
	key   string
	label string
	run   func(*menu) error
}

type menuChoice struct {
	id    string
	label string
}

func runMenu(in io.Reader, out io.Writer, paths []string) error {
	if len(paths) != 0 && len(paths) != 2 {
		return errors.New("menu requires both Commerce and fake provider database paths")
	}
	m := &menu{in: bufio.NewReader(in), out: out}
	var local, provider string
	if len(paths) == 2 {
		local, provider = paths[0], paths[1]
	} else {
		fmt.Fprintln(out, "Billforge 互動式 Commerce Lab。輸入資料庫路徑；既有檔案會繼續使用。")
		var err error
		local, err = m.askDefault("Commerce DB", filepath.Join("billforge-data", "commerce.db"))
		if err != nil {
			return eofIsExit(err)
		}
		provider, err = m.askDefault("Fake provider DB", filepath.Join("billforge-data", "provider.db"))
		if err != nil {
			return eofIsExit(err)
		}
	}
	var err error
	local, err = filepath.Abs(local)
	if err != nil {
		return err
	}
	provider, err = filepath.Abs(provider)
	if err != nil {
		return err
	}
	if local == provider {
		return errors.New("Commerce DB 與 fake provider DB 必須是不同檔案")
	}
	for _, path := range []string{local, provider} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
	}
	l, err := lab.Open(local, provider, func() time.Time {
		if m.fixed != nil {
			return *m.fixed
		}
		return time.Now()
	})
	if err != nil {
		return err
	}
	defer l.Close()
	m.lab = l
	fmt.Fprintf(out, "已開啟 Commerce: %s\nFake provider: %s\n", local, provider)
	return m.loop("主選單", []menuOption{
		{"1", "查詢目前狀態", (*menu).stateMenu},
		{"2", "報價與新購", (*menu).purchaseMenu},
		{"3", "付款", (*menu).paymentMenu},
		{"4", "續約與權益", (*menu).renewalMenu},
		{"5", "發票更正與 credit", (*menu).correctionMenu},
		{"6", "退款", (*menu).refundMenu},
		{"7", "設定模擬時間", (*menu).setClock},
		{"8", "訂閱排程與取消", (*menu).subscriptionMenu},
		{"9", "價格版本與 cohort 遷移", (*menu).catalogMenu},
		{"10", "用量與關帳", (*menu).usageMenu},
		{"11", "企業合約與 Net30", (*menu).contractMenu},
		{"12", "對帳、修復與人工決議", (*menu).reconciliationMenu},
		{"13", "帳戶邊界與灰度遷移", (*menu).accountMigrationMenu},
	}, true)
}

func eofIsExit(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (m *menu) now() time.Time {
	if m.fixed != nil {
		return *m.fixed
	}
	return time.Now()
}

func (m *menu) loop(title string, options []menuOption, root bool) error {
	for {
		fmt.Fprintf(m.out, "\n%s（目前時間：%s）\n", title, m.now().UTC().Format(time.RFC3339))
		for _, option := range options {
			fmt.Fprintf(m.out, " %s. %s\n", option.key, option.label)
		}
		if root {
			fmt.Fprintln(m.out, " 0. 離開")
		} else {
			fmt.Fprintln(m.out, " 0. 返回主選單")
		}
		answer, err := m.ask("選擇")
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if answer == "0" || answer == "" {
			return nil
		}
		found := false
		for _, option := range options {
			if answer != option.key {
				continue
			}
			found = true
			if err := option.run(m); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				fmt.Fprintf(m.out, "操作未完成：%v\n", err)
			}
			break
		}
		if !found {
			fmt.Fprintln(m.out, "無效選項，請輸入選單編號。")
		}
	}
}

func (m *menu) ask(label string) (string, error) {
	fmt.Fprintf(m.out, "%s: ", label)
	line, err := m.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func (m *menu) askDefault(label, defaultValue string) (string, error) {
	value, err := m.ask(label + " [" + defaultValue + "]")
	if err != nil {
		return "", err
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (m *menu) required(label string) (string, error) {
	value, err := m.ask(label)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("%s 不可空白", label)
	}
	return value, nil
}

func (m *menu) amount(label string) (int64, error) {
	return m.positiveInt(label + "（最小貨幣單位；USD 為 cents）")
}

func (m *menu) positiveInt(label string) (int64, error) {
	value, err := m.required(label)
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, errors.New("請輸入正整數")
	}
	return n, nil
}

func (m *menu) requestKey() (string, error) {
	return m.required("請求鍵（重試同一操作時必須重用）")
}

func (m *menu) pick(label string, choices []menuChoice) (string, error) {
	if len(choices) == 0 {
		fmt.Fprintf(m.out, "沒有可選的%s。\n", label)
		return "", nil
	}
	fmt.Fprintf(m.out, "%s：\n", label)
	for i, choice := range choices {
		fmt.Fprintf(m.out, " %d. %s — %s\n", i+1, choice.id, choice.label)
	}
	value, err := m.ask("輸入編號或完整 ID；空白返回")
	if err != nil || value == "" {
		return "", err
	}
	if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= len(choices) {
		return choices[n-1].id, nil
	}
	for _, choice := range choices {
		if value == choice.id {
			return value, nil
		}
	}
	return "", errors.New("找不到該編號或 ID")
}

func (m *menu) print(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(m.out, string(data))
	return err
}

func (m *menu) state() (lab.State, error) {
	return m.lab.State(context.Background())
}

func (m *menu) setClock() error {
	value, err := m.ask("UTC 時間（RFC3339，例如 2026-10-01T00:00:00Z；輸入 real 恢復系統時間）")
	if err != nil {
		return err
	}
	if value == "real" {
		m.fixed = nil
		fmt.Fprintln(m.out, "已恢復系統時間。")
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("時間格式錯誤：%w", err)
	}
	parsed = parsed.UTC()
	m.fixed = &parsed
	fmt.Fprintf(m.out, "模擬時間：%s。這只影響本次 CLI 工作階段；資料庫不會保存時鐘設定。\n", parsed.Format(time.RFC3339))
	return nil
}
