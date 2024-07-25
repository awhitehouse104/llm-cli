package main

import (
  "bufio"
  "context"
  "encoding/json"
  "flag"
  "fmt"
  "os"
  "os/user"
  "strings"

  "github.com/charmbracelet/lipgloss"
  "github.com/charmbracelet/glamour"
  "github.com/sashabaranov/go-openai"
)

type Config struct {
	Model         string `json:"model"`
	AIName        string `json:"ai_name"`
	SystemPrompt  string `json:"system_prompt"`
	Style         string `json:"style"`
}

const (
  cmdQuit =   ":q"
  cmdMulti =  ":multi"
  cmdEnd =    ":end"
  cmdRemove = ":remove"
  cmdFile =   ":file "
)

func loadConfig(path string) (Config, error) {
  var config Config
  file, err := os.Open(path)
  if err != nil {
    return config, err
  }
  defer file.Close()

  decoder := json.NewDecoder(file)
  err = decoder.Decode(&config)
  return config, err
}

func main() {
  config, err := loadConfig("config.json")
  if err != nil {
    fmt.Printf("Error loading config: %v\n", err)
    os.Exit(1)
  }

  var prompt string
  flag.StringVar(&prompt, "prompt", "", "Prompt for the LLM")
  flag.StringVar(&prompt, "p", "", "Prompt shorthand")

  interactive := flag.Bool("interactive", false, "Run in interactive mode")
  flag.BoolVar(interactive, "i", false, "Interactive shorthand")

  flag.Parse()

  apiKey := os.Getenv("OPENAI_API_KEY")
  if apiKey == "" {
    fmt.Println("Error: OPENAI_API_KEY not found in env")
    os.Exit(1)
  }

  client := openai.NewClient(apiKey)

  if *interactive {
    runInteractiveMode(client, config)
  } else {
    if prompt == "" {
      fmt.Println("Error: prompt is required in non-interactive mode")
      flag.Usage()
      os.Exit(1)
    }

    response, err := callOpenAI(client, config, []openai.ChatCompletionMessage{
      {Role: openai.ChatMessageRoleSystem, Content: config.SystemPrompt},
      {Role: openai.ChatMessageRoleUser, Content: prompt},
    })
    if err != nil {
      fmt.Printf("Error: %v\n", err)
      os.Exit(1)
    }

    err = printFormattedResponse(response, config.Style, config.AIName, config.Model)
    if err != nil {
      fmt.Printf("Error formatting response: %v\n", err)
      os.Exit(1)
    }
  }
}

func runInteractiveMode(client *openai.Client, config Config) {
  fmt.Printf("Entering interactive mode. Type %s to exit or %s to enter multiline mode.\n", cmdQuit, cmdMulti)
  fmt.Println()

  scanner := bufio.NewScanner(os.Stdin)
  messages := []openai.ChatCompletionMessage{
    {Role: openai.ChatMessageRoleSystem, Content: config.SystemPrompt},
  }

  var contextFile string
  isMultiline := false
  var lines []string

  for {
    currentDir := getCurrentDirectory()
    inputPrefix := formatInputPrefix(currentDir, isMultiline, config.AIName)
    fmt.Print(inputPrefix)

    if isMultiline {
      fmt.Println()
      for scanner.Scan() {                                                              
        line := scanner.Text()

        if strings.HasPrefix(line, ":") {
          command := strings.ToLower(line)

          if command == cmdMulti {
            fmt.Println("Exiting multiline mode.")
            fmt.Println()
            isMultiline = false
            lines = nil
            break
          }
          if command == cmdRemove {
            if len(lines) > 0 {
              lines = lines[:len(lines)-1]
              fmt.Println("Last line removed.")
              fmt.Println()
            } else {
              fmt.Println("No lines to remove.")
              fmt.Println()
            }
            continue
          }
          if command == cmdEnd {
            break
          }
        }

        lines = append(lines, line)
      }

      if err := scanner.Err(); err != nil {
        fmt.Printf("Error reading input: %v\n", err)
        continue
      }

      if len(lines) > 0 {
        combinedInput := strings.Join(lines, "\n")
        messages = append(messages, openai.ChatCompletionMessage{
          Role: openai.ChatMessageRoleUser,
          Content: combinedInput,
        })

        response, err := callOpenAI(client, config, messages)
        if err != nil {
          fmt.Printf("Error communicating with AI: %v\n", err)
          continue
        }

        messages = append(messages, openai.ChatCompletionMessage{
          Role: openai.ChatMessageRoleAssistant,
          Content: response,
        })

        err = printFormattedResponse(response, config.Style, config.AIName, config.Model)
        if err != nil {
          fmt.Printf("Error formatting response: %v\n", err)
        }
        fmt.Println();
      }
    } else {
      scanner.Scan()
      userInput := scanner.Text()

      if strings.ToLower(userInput) == cmdQuit {
        fmt.Println("Exiting interactive mode.")
        fmt.Println()
        return
      }
      if strings.ToLower(userInput) == cmdMulti {
        fmt.Printf("Multiline mode. Type %s to finish input, %s to delete the most recent line.\n", cmdEnd, cmdRemove)
        fmt.Println()
        isMultiline = true
        lines = nil
        continue
      }
      if strings.HasPrefix(userInput, cmdFile) {
        fileName := strings.TrimPrefix(userInput, cmdFile)
        content, err := readFile(fileName)
        if err != nil {
          fmt.Printf("Error reading file: %v\n", err)
          continue
        }
        contextFile = fileName
        fileContext := fmt.Sprintf("Content of %s:\n%s", fileName, content)
        messages = append(messages, openai.ChatCompletionMessage{
          Role: openai.ChatMessageRoleUser,
          Content: fileContext,
        })
        fmt.Printf("Added %s to the context.\n", fileName)
        fmt.Println()
        continue
      }

      userMessage := userInput
      if contextFile != "" {
        userMessage = fmt.Sprintf("(Context: %s) %s", contextFile, userInput)
      }

      messages = append(messages, openai.ChatCompletionMessage{
        Role: openai.ChatMessageRoleUser,
        Content: userMessage,
      })

      response, err := callOpenAI(client, config, messages)
      if err != nil {
        fmt.Printf("Error: %v\n", err)
        continue
      }

      messages = append(messages, openai.ChatCompletionMessage{
        Role: openai.ChatMessageRoleAssistant,
        Content: response,
      })

      err = printFormattedResponse(response, config.Style, config.AIName, config.Model)
      if err != nil {
        fmt.Printf("Error formatting response: %v\n", err)
      }
      fmt.Println()
    }
  }
}

func formatInputPrefix(dir string, isMultiline bool, aiName string) string {
	dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	youStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("183")).Bold(true)
	multilineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("204")) 

	formattedDir := dirStyle.Render(fmt.Sprintf("(%s)", dir))
	formattedYou := youStyle.Render("You")
	
	if isMultiline {
		multilineIndicator := multilineStyle.Render("[Multiline]")
		return fmt.Sprintf("%s %s %s: ", formattedDir, multilineIndicator, formattedYou)
	}
	
	return fmt.Sprintf("%s %s: ", formattedDir, formattedYou)
}

func getCurrentDirectory() string {
  currentDir, err := os.Getwd()
  if err != nil {
    return "unknown"
  }

  usr, err := user.Current()
  if err == nil && strings.HasPrefix(currentDir, usr.HomeDir) {
    return "~" + strings.TrimPrefix(currentDir, usr.HomeDir)
  }

  return currentDir
}

func readFile(fileName string) (string, error) {
  content, err := os.ReadFile(fileName)
  if err != nil {
    return "", err
  }
  return string(content), nil
}

func callOpenAI(client *openai.Client, config Config, messages []openai.ChatCompletionMessage) (string, error) {
  resp, err := client.CreateChatCompletion(
    context.Background(),
    openai.ChatCompletionRequest{
      Model: config.Model,
      Messages: messages,
    },
  )

  if err != nil {
    return "", err
  }

  return resp.Choices[0].Message.Content, nil
}

func printFormattedResponse(response, style, aiName, model string) error {
	r, err := glamour.NewTermRenderer(
		glamour.WithStylePath(fmt.Sprintf("./styles/%s.json", style)),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		return err
	}

	aiNameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	modelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("178"))

	formattedAIName := aiNameStyle.Render(aiName)
	formattedModel := modelStyle.Render(fmt.Sprintf("(%s)", model))

  fmt.Printf("\n%s %s: ", formattedAIName, formattedModel)

	out, err := r.Render(response)
	if err != nil {
		return err
	}

	fmt.Print(out)
	return nil
}
