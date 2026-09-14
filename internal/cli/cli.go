package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"mem_cli/internal/app"
	"mem_cli/internal/domain"
	"mem_cli/internal/sqlite"
)

const schemaVersion = 2

type Response struct {
	OK      bool           `json:"ok"`
	Command string         `json:"command,omitempty"`
	Data    any            `json:"data,omitempty"`
	Error   *ResponseError `json:"error,omitempty"`
	Meta    ResponseMeta   `json:"meta"`
}

type ResponseError struct {
	Code       string                    `json:"code"`
	Message    string                    `json:"message"`
	Candidates []string                  `json:"candidates,omitempty"`
	Subjects   []domain.SubjectCandidate `json:"subjects,omitempty"`
}

type ResponseMeta struct {
	SchemaVersion int `json:"schema_version"`
}

type printedError struct {
	exitCode int
}

func (e *printedError) Error() string {
	return "output already printed"
}

type options struct {
	databasePath   string
	pretty         bool
	recursive      bool
	namespace      string
	subject        string
	memoryType     string
	content        string
	reason         string
	tags           []string
	source         string
	expiresAt      string
	metadataJSON   string
	query          string
	tag            string
	limit          int
	format         string
	targetMemory   string
	arguments      []string
	changed        map[string]bool
	clearExpiry    bool
	subtree        bool
	matchMode      string
	includeExpired bool
	contentFile    string
	related        []string
	relation       string
	relatedTo      string
	version        int64
}

var currentCommand string

func Execute() int {
	root, options := NewRootCommand()
	return executeRoot(root, options)
}

func Run(arguments []string, stdout, stderr *bytes.Buffer) int {
	root, options := NewRootCommand()
	root.SetArgs(arguments)
	root.SetOut(stdout)
	root.SetErr(stderr)
	return executeRoot(root, options)
}

func executeRoot(root *cobra.Command, options *options) int {
	err := root.Execute()
	if err == nil {
		return 0
	}
	var printed *printedError
	if errors.As(err, &printed) {
		return printed.exitCode
	}
	exitCode := exitCodeForError(err)
	printError(root.ErrOrStderr(), options.pretty, err)
	return exitCode
}

func NewRootCommand() (*cobra.Command, *options) {
	options := &options{limit: 10}
	root := &cobra.Command{
		Use:           "mem",
		Short:         "Local-first persistent memory layer for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(command *cobra.Command, args []string) {
			currentCommand = responseCommandName(command)
		},
	}
	home, err := os.UserHomeDir()
	defaultDatabase := "mem.db"
	if err == nil {
		defaultDatabase = filepath.Join(home, ".mem_cli", "mem.db")
	}
	root.PersistentFlags().StringVar(&options.databasePath, "db", defaultDatabase, "SQLite database path")
	root.PersistentFlags().BoolVar(&options.pretty, "pretty", false, "pretty-print JSON output")

	root.AddCommand(namespaceCommand(options))
	root.AddCommand(addCommand(options))
	root.AddCommand(getCommand(options))
	root.AddCommand(listCommand(options))
	root.AddCommand(searchCommand(options))
	root.AddCommand(updateCommand(options))
	root.AddCommand(forgetCommand(options))
	root.AddCommand(historyCommand(options))
	root.AddCommand(revertCommand(options))
	root.AddCommand(contextCommand(options))
	root.AddCommand(importCommand(options))
	root.AddCommand(exportCommand(options))
	return root, options
}

func namespaceCommand(options *options) *cobra.Command {
	command := &cobra.Command{Use: "ns", Short: "Manage namespaces"}
	create := &cobra.Command{
		Use:   "create <path>",
		Args:  cobra.ExactArgs(1),
		Short: "Create a namespace hierarchy",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			namespace, created, err := service.CreateNamespace(ctx, options.args()[0])
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": namespace.ID, "name": namespace.NormalizedName, "created": created}, nil
		}),
	}
	list := &cobra.Command{
		Use:   "list [prefix]",
		Args:  cobra.MaximumNArgs(1),
		Short: "List namespaces",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			prefix := ""
			if len(options.args()) > 0 {
				prefix = options.args()[0]
			}
			namespaces, err := service.ListNamespaces(ctx, prefix)
			if err != nil {
				return nil, err
			}
			return map[string]any{"namespaces": namespaces}, nil
		}),
	}
	get := &cobra.Command{
		Use:   "get <id|path>",
		Args:  cobra.ExactArgs(1),
		Short: "Get a namespace",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			return service.GetNamespace(ctx, options.args()[0])
		}),
	}
	delete := &cobra.Command{
		Use:   "delete <id|path>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a namespace",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			target := options.args()[0]
			if err := service.DeleteNamespace(ctx, target, options.recursive); err != nil {
				return nil, err
			}
			return map[string]any{"target": target, "deleted": true}, nil
		}),
	}
	delete.Flags().BoolVar(&options.recursive, "recursive", false, "recursively delete child namespaces and their memories")
	command.AddCommand(create, list, get, delete)
	return command
}

func addCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "add",
		Short: "Add a memory",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			metadata, err := parseMetadata(options.metadataJSON)
			if err != nil {
				return nil, err
			}
			memoryType := "fact"
			if options.changed["type"] {
				memoryType = options.memoryType
			}
			content, err := readContentInput(options.changed["file"], options.contentFile, options.content)
			if err != nil {
				return nil, err
			}
			memory, err := service.AddMemory(ctx, app.AddInput{
				Namespace:  options.namespace,
				Subject:    options.subject,
				Type:       memoryType,
				Content:    content,
				Reason:     options.reason,
				Tags:       options.tags,
				Metadata:   metadata,
				Source:     options.source,
				ExpiresAt:  options.expiresAt,
				RelatedIDs: options.related,
				Relation:   options.relation,
			})
			if err != nil {
				return nil, err
			}
			if len(options.related) > 0 {
				return service.GetMemory(ctx, memory.ID, false)
			}
			return memory, nil
		}),
	}
	addMemoryFlags(command, options)
	return command
}

func getCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "get <memory-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Get a full memory",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			return service.GetMemory(ctx, options.args()[0], options.includeExpired)
		}),
	}
	command.Flags().BoolVar(&options.includeExpired, "include-expired", false, "include an expired memory when accessed by ID")
	return command
}

func listCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "list",
		Short: "List memories in a namespace",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			var memoryType string
			if options.changed["type"] {
				memoryType = options.memoryType
			}
			list, err := service.ListMemories(ctx, app.MemoryFilter{
				NamespaceID: options.namespace,
				Subject:     options.subject,
				Type:        domain.MemoryType(memoryType),
				Tag:         options.tag,
				Subtree:     options.subtree,
				RelatedTo:   options.relatedTo,
				Limit:       options.limit,
			})
			if err != nil {
				return nil, err
			}
			summaries := make([]domain.MemorySummary, 0, len(list))
			for _, memory := range list {
				summaries = append(summaries, memorySummary(memory))
			}
			return map[string]any{"memories": summaries}, nil
		}),
	}
	addNamespaceFlag(command, options)
	addFilterFlags(command, options)
	command.Flags().StringVar(&options.relatedTo, "related-to", "", "include memories connected in either direction to this memory id")
	command.Flags().BoolVar(&options.subtree, "subtree", true, "include memories from descendant namespaces")
	command.Flags().IntVar(&options.limit, "limit", 100, "maximum number of memories")
	return command
}

func searchCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "search",
		Short: "Search memories with FTS5",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			results, err := service.SearchMemories(ctx, searchFilter(options, "all"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"results": results}, nil
		}),
	}
	addNamespaceFlag(command, options)
	command.Flags().StringVar(&options.query, "query", "", "full-text query")
	command.Flags().StringVar(&options.matchMode, "match", "all", "term matching: all or any")
	addFilterFlags(command, options)
	command.Flags().BoolVar(&options.subtree, "subtree", true, "include memories from descendant namespaces")
	command.Flags().IntVar(&options.limit, "limit", 10, "maximum number of results")
	_ = command.MarkFlagRequired("query")
	return command
}

func updateCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "update <memory-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Update mutable memory fields",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			input := app.UpdateInput{ID: options.args()[0]}
			if options.changed["subject"] {
				input.Subject = &options.subject
			}
			if options.changed["type"] {
				input.Type = &options.memoryType
			}
			if options.changed["content"] {
				input.Content = &options.content
			}
			if options.changed["reason"] {
				input.Reason = &options.reason
			}
			if options.changed["tags"] {
				input.Tags = options.tags
				input.TagsSet = true
			}
			if options.changed["metadata"] {
				metadata, err := parseMetadata(options.metadataJSON)
				if err != nil {
					return nil, err
				}
				input.Metadata = metadata
				input.MetadataSet = true
			}
			if options.changed["source"] {
				input.Source = &options.source
			}
			if options.changed["expires-at"] {
				input.ExpiresAt = &options.expiresAt
			}
			if options.changed["clear-expiry"] && options.clearExpiry {
				input.ClearExpiry = true
			}
			if options.changed["file"] {
				content, err := readContentInput(true, options.contentFile, "")
				if err != nil {
					return nil, err
				}
				input.Content = &content
			} else if options.changed["content"] {
				input.Content = &options.content
			}
			if options.changed["related"] {
				input.RelatedIDs = options.related
				input.RelatedSet = true
				input.Relation = options.relation
			}
			return service.UpdateMemory(ctx, input)
		}),
	}
	command.Flags().StringVar(&options.subject, "subject", "", "memory subject")
	command.Flags().StringVar(&options.memoryType, "type", "", "memory type")
	command.Flags().StringVar(&options.content, "content", "", "memory content")
	command.Flags().StringVar(&options.contentFile, "file", "", "read memory content from file path or - for stdin")
	command.Flags().StringSliceVar(&options.related, "related", nil, "comma-separated memory ids or exact subjects in the namespace")
	command.Flags().StringVar(&options.relation, "relation", "related", "relation label for related ids")
	command.Flags().StringVar(&options.reason, "reason", "", "memory reason")
	command.Flags().StringSliceVar(&options.tags, "tags", nil, "comma-separated tags")
	command.Flags().StringVar(&options.metadataJSON, "metadata", "", "JSON object metadata")
	command.Flags().StringVar(&options.source, "source", "", "memory source path or identifier")
	command.Flags().StringVar(&options.expiresAt, "expires-at", "", "expiration time in RFC3339")
	command.Flags().BoolVar(&options.clearExpiry, "clear-expiry", false, "remove expiration")
	return command
}

func forgetCommand(options *options) *cobra.Command {
	return &cobra.Command{
		Use:   "forget <memory-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a memory",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			id := options.args()[0]
			if err := service.ForgetMemory(ctx, id); err != nil {
				return nil, err
			}
			return map[string]any{"id": id, "forgotten": true}, nil
		}),
	}
}

func historyCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "history <memory-id>",
		Args:  cobra.ExactArgs(1),
		Short: "List memory versions or show one full version",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			id := options.args()[0]
			if options.changed["version"] {
				snapshot, err := service.MemoryVersion(ctx, id, options.version)
				if err != nil {
					return nil, err
				}
				return map[string]any{"id": id, "version": snapshot}, nil
			}
			versions, err := service.MemoryHistory(ctx, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": id, "versions": versions}, nil
		}),
	}
	command.Flags().Int64Var(&options.version, "version", 0, "version number to display")
	return command
}

func revertCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "revert <memory-id> --version N",
		Args:  cobra.ExactArgs(1),
		Short: "Restore a memory from a saved version",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			return service.RevertMemory(ctx, options.args()[0], options.version)
		}),
	}
	command.Flags().Int64Var(&options.version, "version", 0, "version number to restore")
	_ = command.MarkFlagRequired("version")
	return command
}

func contextCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "context",
		Short: "Retrieve compact agent context",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			results, err := service.SearchMemories(ctx, searchFilter(options, "any"))
			if err != nil {
				return nil, err
			}
			return map[string]any{"memories": results}, nil
		}),
	}
	addNamespaceFlag(command, options)
	command.Flags().StringVar(&options.query, "query", "", "natural-language or keyword query")
	command.Flags().StringVar(&options.matchMode, "match", "any", "term matching: all or any")
	addFilterFlags(command, options)
	command.Flags().BoolVar(&options.subtree, "subtree", true, "include memories from descendant namespaces")
	command.Flags().IntVar(&options.limit, "limit", 10, "maximum number of context items")
	_ = command.MarkFlagRequired("query")
	return command
}

func importCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "import <file>",
		Args:  cobra.ExactArgs(1),
		Short: "Import one file as one memory",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			metadata, err := parseMetadata(options.metadataJSON)
			if err != nil {
				return nil, err
			}
			memoryType := "doc"
			if options.changed["type"] {
				memoryType = options.memoryType
			}
			memory, err := service.ImportFile(ctx, options.args()[0], options.namespace, options.subject, memoryType, options.tags, metadata, options.expiresAt, options.related, options.relation)
			if err != nil {
				return nil, err
			}
			if len(options.related) > 0 {
				return service.GetMemory(ctx, memory.ID, false)
			}
			return memory, nil
		}),
	}
	addNamespaceFlag(command, options)
	command.Flags().StringVar(&options.subject, "subject", "", "memory subject")
	command.Flags().StringVar(&options.memoryType, "type", "doc", "memory type")
	command.Flags().StringSliceVar(&options.related, "related", nil, "comma-separated memory ids or exact subjects in the namespace")
	command.Flags().StringVar(&options.relation, "relation", "related", "relation label for related memories")
	command.Flags().StringSliceVar(&options.tags, "tags", nil, "comma-separated tags")
	command.Flags().StringVar(&options.metadataJSON, "metadata", "", "JSON object metadata")
	command.Flags().StringVar(&options.expiresAt, "expires-at", "", "expiration time in RFC3339")
	return command
}

func exportCommand(options *options) *cobra.Command {
	command := &cobra.Command{
		Use:   "export",
		Short: "Export namespace memories",
		RunE: execute(options, func(ctx context.Context, service *app.Service) (any, error) {
			memories, err := service.ExportMemories(ctx, options.namespace, options.subtree)
			if err != nil {
				return nil, err
			}
			if options.format == "markdown" {
				return renderMarkdown(options.namespace, memories), nil
			}
			return map[string]any{"namespace": options.namespace, "memories": memories}, nil
		}),
	}
	addNamespaceFlag(command, options)
	command.Flags().BoolVar(&options.subtree, "subtree", true, "include memories from descendant namespaces")
	command.Flags().StringVar(&options.format, "format", "json", "export format: json or markdown")
	_ = command.MarkFlagRequired("namespace")
	return command
}

func execute(options *options, action func(context.Context, *app.Service) (any, error)) func(*cobra.Command, []string) error {
	return func(command *cobra.Command, arguments []string) error {
		options.setArgs(arguments)
		changed := make(map[string]bool)
		command.Flags().Visit(func(flag *pflag.Flag) {
			changed[flag.Name] = true
		})
		options.changed = changed
		repository, err := sqlite.Open(options.databasePath)
		if err != nil {
			return printServiceError(command.ErrOrStderr(), options.pretty, err)
		}
		defer repository.Close()
		service := app.NewService(repository.NamespaceRepository(), repository.MemoryRepository())
		data, err := action(context.Background(), service)
		if err != nil {
			return printServiceError(command.ErrOrStderr(), options.pretty, err)
		}
		if format, formatOK := data.(markdownOutput); formatOK {
			_, writeErr := fmt.Fprint(command.OutOrStdout(), string(format))
			if writeErr != nil {
				return writeErr
			}
			return nil
		}
		return printResponse(command.OutOrStdout(), options.pretty, responseCommandName(command), Response{OK: true, Command: responseCommandName(command), Data: data, Meta: ResponseMeta{SchemaVersion: schemaVersion}})
	}
}

type markdownOutput string

func renderMarkdown(namespace string, memories []domain.Memory) markdownOutput {
	var builder strings.Builder
	for _, memory := range memories {
		fmt.Fprintf(&builder, "# %s\n\n", memory.Subject)
		fmt.Fprintf(&builder, "- id: %s\n- type: %s\n- namespace: %s\n- created_at: %s\n", memory.ID, memory.Type, namespace, memory.CreatedAt)
		if memory.Reason != "" {
			fmt.Fprintf(&builder, "- reason: %s\n", memory.Reason)
		}
		if len(memory.Tags) > 0 {
			fmt.Fprintf(&builder, "- tags: %s\n", strings.Join(memory.Tags, ", "))
		}
		if len(memory.Source) > 0 {
			source, _ := json.Marshal(memory.Source)
			fmt.Fprintf(&builder, "- source: %s\n", source)
		}
		builder.WriteString("\n```text\n")
		builder.WriteString(memory.Content)
		builder.WriteString("\n```\n\n")
	}
	return markdownOutput(builder.String())
}

func searchFilter(options *options, matchMode string) app.SearchFilter {
	var memoryType string
	if options.changed["type"] {
		memoryType = options.memoryType
	}
	return app.SearchFilter{
		Query:       options.query,
		NamespaceID: options.namespace,
		Subject:     options.subject,
		Type:        domain.MemoryType(memoryType),
		Tag:         options.tag,
		Subtree:     options.subtree,
		MatchMode:   matchMode,
		Limit:       options.limit,
	}
}

func addMemoryFlags(command *cobra.Command, options *options) {
	command.Flags().StringVar(&options.namespace, "namespace", "", "namespace path")
	command.Flags().StringVar(&options.subject, "subject", "", "memory subject")
	command.Flags().StringVar(&options.memoryType, "type", "fact", "memory type")
	command.Flags().StringVar(&options.content, "content", "", "memory content")
	command.Flags().StringVar(&options.reason, "reason", "", "reason the memory is stored")
	command.Flags().StringSliceVar(&options.tags, "tags", nil, "comma-separated tags")
	command.Flags().StringVar(&options.source, "source", "", "memory source path or identifier")
	command.Flags().StringVar(&options.expiresAt, "expires-at", "", "expiration time in RFC3339")
	command.Flags().StringVar(&options.metadataJSON, "metadata", "", "JSON object metadata")
	command.Flags().StringVar(&options.contentFile, "file", "", "read memory content from file path or - for stdin")
	command.Flags().StringSliceVar(&options.related, "related", nil, "comma-separated memory ids or exact subjects in the namespace")
	command.Flags().StringVar(&options.relation, "relation", "related", "relation label for related ids")
	_ = command.MarkFlagRequired("namespace")
	_ = command.MarkFlagRequired("subject")
}

func addNamespaceFlag(command *cobra.Command, options *options) {
	command.Flags().StringVar(&options.namespace, "namespace", "", "namespace path")
}

func addFilterFlags(command *cobra.Command, options *options) {
	command.Flags().StringVar(&options.subject, "subject", "", "filter by subject")
	command.Flags().StringVar(&options.memoryType, "type", "", "filter by memory type")
	command.Flags().StringVar(&options.tag, "tag", "", "filter by tag")
}

func (options *options) args() []string {
	return options.arguments
}

func (options *options) setArgs(arguments []string) {
	options.arguments = arguments
}

func parseMetadata(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(value), &metadata); err != nil {
		return nil, domain.NewInvalidArgumentError("metadata must be a JSON object")
	}
	return metadata, nil
}

func readContentInput(specified bool, path, fallback string) (string, error) {
	if !specified {
		return fallback, nil
	}
	if path == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", domain.NewImportError("unable to read content from stdin", err)
		}
		return string(content), nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", domain.NewImportError("unable to read content file", err)
	}
	return string(content), nil
}

func memorySummary(memory domain.Memory) domain.MemorySummary {
	replacer := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")
	runes := []rune(replacer.Replace(memory.Content))
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return domain.MemorySummary{
		ID: memory.ID, NamespaceID: memory.NamespaceID, Subject: memory.Subject, Type: memory.Type,
		Snippet: string(runes), Reason: memory.Reason, Tags: memory.Tags, Metadata: memory.Metadata,
		Source: memory.Source, CreatedAt: memory.CreatedAt, UpdatedAt: memory.UpdatedAt, ExpiresAt: memory.ExpiresAt,
	}
}

func responseCommandName(command *cobra.Command) string {
	if command == nil {
		return currentCommand
	}
	parts := strings.Split(strings.TrimSpace(command.CommandPath()), " ")
	if len(parts) > 1 {
		parts = parts[1:]
	}
	name := strings.Join(parts, ".")
	if name == "" {
		return currentCommand
	}
	return name
}

func printServiceError(output io.Writer, pretty bool, err error) error {
	printError(output, pretty, err)
	return &printedError{exitCode: exitCodeForError(err)}
}

func printError(output io.Writer, pretty bool, err error) {
	var domainError *domain.Error
	if !errors.As(err, &domainError) {
		domainError = &domain.Error{Code: domain.ErrorInvalidArgument, Message: err.Error()}
	}
	response := Response{OK: false, Error: &ResponseError{Code: domainError.Code, Message: domainError.Message, Candidates: domainError.Candidates, Subjects: domainError.SubjectCandidates}, Meta: ResponseMeta{SchemaVersion: schemaVersion}}
	_ = printResponse(output, pretty, currentCommand, response)
}

func printResponse(output io.Writer, pretty bool, command string, response Response) error {
	if response.Command == "" {
		response.Command = command
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(response)
}

func exitCodeForError(err error) int {
	var domainError *domain.Error
	switch {
	case errors.As(err, &domainError):
		switch domainError.Code {
		case domain.ErrorInvalidArgument, domain.ErrorInvalidNamespace, domain.ErrorInvalidMemoryType, domain.ErrorImport, domain.ErrorAmbiguousID, domain.ErrorAmbiguousSubject:
			return 2
		case domain.ErrorNamespaceNotFound, domain.ErrorMemoryNotFound:
			return 3
		case domain.ErrorNamespaceConflict, domain.ErrorPossibleDuplicateNamespace:
			return 4
		default:
			return 1
		}
	default:
		return 2
	}
}
