package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	// "github.com/charmbracelet/bubbles/viewport" // 未使用になったためコメントアウトまたは削除
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/ini.v1"
)

// awsProfile はAWSプロファイルの情報を保持します。
type awsProfile struct {
	Name    string // プロファイル名
	RoleArn string // role_arn (存在すれば)
}

// headerHeight はビューポートの計算に使用するヘッダーの行数です。
// 1行目: タイトル, 2行目: 区切り線
const headerHeight = 2

// footerHeight はビューポートの計算に使用するフッターの行数です。
// Viewメソッド内のフッター構成 (3行):
// 1. 区切り線 (ビューポートの直後)
// 2. ヘルプテキスト
// 3. ステータス情報
const footerHeight = 3

// model はアプリケーションの状態を保持します。
type model struct {
	profiles          []awsProfile // 利用可能なAWSプロファイルのリスト
	cursor            int          // 現在選択されているプロファイルのインデックス
	scrollOffset      int          // リスト表示のスクロールオフセット（開始インデックス）
	listVisibleHeight int          // リストが表示される実際の高さ（行数）
	windowWidth       int          // 現在のウィンドウ幅
	showRoleArn       bool         // role_arn を表示するかどうかのフラグ
	selectedProfile   string       // ユーザーによって最終的に選択されたプロファイル名
	quitting          bool         // ユーザーがqキーやCtrl+Cで終了しようとしているか
	err               error        // 初期化時などに発生したエラー
	ready             bool         // WindowSizeMsgを一度受信してlistVisibleHeightが設定されたか
}

// loadAWSProfiles は ~/.aws/config ファイルを読み込み、プロファイル情報を抽出します。
func loadAWSProfiles() ([]awsProfile, error) {
	usr, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("ユーザーホームディレクトリの取得に失敗しました: %w", err)
	}
	configFile := filepath.Join(usr.HomeDir, ".aws", "config")

	cfg, err := ini.Load(configFile)
	if err != nil {
		return nil, fmt.Errorf("~/.aws/config の読み込みに失敗しました: %w (ファイル: %s)", err, configFile)
	}

	var profiles []awsProfile
	for _, section := range cfg.Sections() {
		sectionName := section.Name()
		var profileName string

		if sectionName == ini.DefaultSection {
			if section.HasKey("aws_access_key_id") || section.HasKey("sso_session") || section.HasKey("role_arn") {
				profileName = "default"
			} else {
				continue
			}
		} else if strings.HasPrefix(sectionName, "profile ") {
			profileName = strings.TrimSpace(strings.TrimPrefix(sectionName, "profile "))
		} else {
			profileName = sectionName
		}

		if strings.TrimSpace(profileName) == "" {
			continue
		}

		profiles = append(profiles, awsProfile{
			Name:    profileName,
			RoleArn: section.Key("role_arn").String(),
		})
	}
	return profiles, nil
}

// initialModel はアプリケーションの初期状態を生成します。
func initialModel() model {
	profiles, err := loadAWSProfiles()
	initialCursor := 0

	// 環境変数 AWS_DEFAULT_PROFILE を読み込み、初期カーソル位置を設定
	currentProfileEnv := os.Getenv("AWS_DEFAULT_PROFILE")
	if currentProfileEnv != "" && err == nil { // エラーがない場合のみプロファイル検索
		for i, p := range profiles {
			if p.Name == currentProfileEnv {
				initialCursor = i
				break
			}
		}
	}

	return model{
		profiles:     profiles,
		cursor:       initialCursor, // ★★★ 初期カーソルを設定 ★★★
		err:          err,
		scrollOffset: 0, // 初期スクロールオフセットは0
		showRoleArn:  false,
		ready:        false, // まだウィンドウサイズが不明
	}
}

// Init はモデル初期化時に実行されるコマンドを返します。
func (m model) Init() tea.Cmd {
	return nil
}

// Update はイベントに基づいてモデルを更新し、コマンドを返します。
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.err != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, nil
	}

	if len(m.profiles) == 0 && m.ready {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "ctrl+c", "q", "enter":
				m.quitting = true
				return m, tea.Quit
			}
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		prevListVisibleHeight := m.listVisibleHeight // 以前の高さを保持 (初回は0)
		m.listVisibleHeight = msg.Height - headerHeight - footerHeight
		if m.listVisibleHeight < 0 {
			m.listVisibleHeight = 0
		}

		isFirstReady := !m.ready // これが最初のWindowSizeMsgかどうかのフラグ
		if !m.ready {
			m.ready = true
		}

		// ウィンドウリサイズ時または最初の準備完了時のスクロールオフセットとカーソルの調整
		if len(m.profiles) > 0 {
			// ★★★ 最初の準備完了時に初期カーソルが表示されるようにスクロールオフセットを調整 ★★★
			if isFirstReady && m.listVisibleHeight > 0 {
				if m.cursor >= m.listVisibleHeight {
					// 初期カーソルが表示範囲より下にある場合、カーソルが表示範囲の最後に来るようにオフセット調整
					m.scrollOffset = m.cursor - m.listVisibleHeight + 1
				} else {
					// 初期カーソルが表示範囲内にある場合はオフセットは0のまま
					m.scrollOffset = 0
				}
			} else if !isFirstReady && prevListVisibleHeight != m.listVisibleHeight { // リサイズの場合
				// スクロールオフセットがコンテンツの最後を超えないように調整
				if m.scrollOffset+m.listVisibleHeight > len(m.profiles) {
					m.scrollOffset = len(m.profiles) - m.listVisibleHeight
				}
			}

			// 共通のオフセットとカーソルの境界チェック
			if m.scrollOffset < 0 {
				m.scrollOffset = 0
			}
			// 最大スクロールオフセットの計算 (リストが短い場合は0になる)
			maxScrollOffset := len(m.profiles) - m.listVisibleHeight
			if maxScrollOffset < 0 {
				maxScrollOffset = 0
			}
			if m.scrollOffset > maxScrollOffset {
				m.scrollOffset = maxScrollOffset
			}


			// カーソルが表示範囲外に出ないように調整
			if m.cursor < m.scrollOffset { // カーソルがオフセットより上に行ってしまった場合
				m.cursor = m.scrollOffset
			}
			if m.listVisibleHeight > 0 && m.cursor >= m.scrollOffset+m.listVisibleHeight { // カーソルがオフセット+表示高さより下に行ってしまった場合
				m.cursor = m.scrollOffset + m.listVisibleHeight - 1
			}
			// カーソルがプロファイル数を超えないように
			if m.cursor >= len(m.profiles) {
				m.cursor = len(m.profiles) -1
			}
            if m.cursor < 0 && len(m.profiles) > 0 { // プロファイルがあるのにカーソルが負の場合
                m.cursor = 0
            }
		}


	case tea.KeyMsg:
		if len(m.profiles) == 0 {
			if msg.String() == "ctrl+c" || msg.String() == "q" || msg.String() == "enter" {
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				if m.cursor < m.scrollOffset {
					m.scrollOffset = m.cursor
				}
			}
		case "down", "j":
			if m.cursor < len(m.profiles)-1 {
				m.cursor++
				if m.listVisibleHeight > 0 && m.cursor >= m.scrollOffset+m.listVisibleHeight {
					m.scrollOffset = m.cursor - m.listVisibleHeight + 1
				}
			}
		case "v":
			m.showRoleArn = !m.showRoleArn
		case "enter":
			if len(m.profiles) > 0 {
				m.selectedProfile = m.profiles[m.cursor].Name
			} else {
				m.quitting = true
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

// View は現在のモデルの状態に基づいてUIを描画し、文字列として返します。
func (m model) View() string {
	if m.quitting || m.selectedProfile != "" {
		return ""
	}

	if m.err != nil {
		errorStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
		return fmt.Sprintf("\n%s\n\n qキーまたはCtrl+Cで終了します。\n", errorStyle.Render(fmt.Sprintf("初期化エラー: %v", m.err)))
	}

	if !m.ready {
		return "Initializing, please wait..."
	}

	if len(m.profiles) == 0 {
		infoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
		return fmt.Sprintf("\n%s\n\n qキー、Ctrl+C、またはEnterキーで終了します。\n", infoStyle.Render("利用可能なAWSプロファイルが見つかりませんでした。"))
	}

	var s strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	s.WriteString(titleStyle.Render("AWSプロファイルを選択してください") + "\n")
	s.WriteString(lipgloss.NewStyle().Faint(true).Render(strings.Repeat("─", m.windowWidth)) + "\n")

	if m.listVisibleHeight <= 0 {
		s.WriteString(lipgloss.NewStyle().Italic(true).Render("ウィンドウサイズが小さすぎます。") + "\n")
	} else {
		start := m.scrollOffset
		end := m.scrollOffset + m.listVisibleHeight
		if end > len(m.profiles) {
			end = len(m.profiles)
		}
		if start > end { // リストが非常に短いか空の場合の安全策
			start = end
		}

		for i := start; i < end; i++ {
			// プロファイルリストが空でないことを確認 (start/end 計算後だが念のため)
			if i < 0 || i >= len(m.profiles) {
				continue
			}
			p := m.profiles[i]
			nameStyle := lipgloss.NewStyle()
			roleArnStyle := lipgloss.NewStyle().Faint(true).Italic(true)

			cursorText := "  "
			if m.cursor == i {
				cursorText = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).SetString("> ").String()
				nameStyle = nameStyle.Bold(true).Underline(true)
			}

			roleArnDisplay := ""
			if m.showRoleArn && m.cursor == i && p.RoleArn != "" {
				roleArnDisplay = roleArnStyle.Render(fmt.Sprintf(" (RoleARN: %s)", p.RoleArn))
			}
			s.WriteString(fmt.Sprintf("%s%s%s\n", cursorText, nameStyle.Render(p.Name), roleArnDisplay))
		}
	}

	faintStyle := lipgloss.NewStyle().Faint(true)
	statusText := fmt.Sprintf("プロファイル %d/%d", m.cursor+1, len(m.profiles))
	helpText := "↑/k:上, ↓/j:下, Enter:選択, v:RoleARN表示切替, q/Ctrl+C:終了"

	s.WriteString(faintStyle.Render(strings.Repeat("─", m.windowWidth)) + "\n")
	s.WriteString(faintStyle.Render(helpText) + "\n")
	s.WriteString(faintStyle.Render(statusText))

	return s.String()
}

// main はプログラムのエントリーポイントです。
func main() {
	program := tea.NewProgram(initialModel(), tea.WithOutput(os.Stderr), tea.WithAltScreen())

	finalModel, err := program.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "CLIアプリケーションの実行に失敗しました: %v\n", err)
		os.Exit(1)
	}

	m, ok := finalModel.(model)
	if !ok {
		fmt.Fprintln(os.Stderr, "モデルの型変換中に予期せぬエラーが発生しました。")
		os.Exit(1)
	}

	if m.err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %v\n", m.err)
		os.Exit(1)
	}

	if m.selectedProfile != "" && !m.quitting {
		fmt.Printf("export AWS_DEFAULT_PROFILE=%s\n", m.selectedProfile)
		os.Exit(0)
	}

	if m.selectedProfile == "" && (m.quitting || len(m.profiles) == 0) {
		if len(m.profiles) == 0 && !m.quitting {
			fmt.Fprintln(os.Stderr, "利用可能なAWSプロファイルがありませんでした。")
		} else if m.quitting {
			fmt.Fprintln(os.Stderr, "プロファイルの選択がキャンセルされました。")
		}
		os.Exit(1)
	}
	os.Exit(1)
}
