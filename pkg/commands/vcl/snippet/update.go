package snippet

import (
	"fmt"
	"io"

	"github.com/fastly/cli/pkg/cmd"
	"github.com/fastly/cli/pkg/config"
	"github.com/fastly/cli/pkg/errors"
	"github.com/fastly/cli/pkg/manifest"
	"github.com/fastly/cli/pkg/text"
	"github.com/fastly/go-fastly/v6/fastly"
)

// NewUpdateCommand returns a usable command registered under the parent.
func NewUpdateCommand(parent cmd.Registerer, globals *config.Data, data manifest.Data) *UpdateCommand {
	var c UpdateCommand
	c.CmdClause = parent.Command("update", "Update a VCL snippet for a particular service and version")
	c.Globals = globals
	c.manifest = data

	// Required flags
	c.RegisterFlag(cmd.StringFlagOpts{
		Name:        cmd.FlagVersionName,
		Description: cmd.FlagVersionDesc,
		Dst:         &c.serviceVersion.Value,
		Required:    true,
	})

	// Optional flags
	c.RegisterAutoCloneFlag(cmd.AutoCloneFlagOpts{
		Action: c.autoClone.Set,
		Dst:    &c.autoClone.Value,
	})
	c.CmdClause.Flag("content", "VCL snippet passed as file path or content, e.g. $(< snippet.vcl)").Action(c.content.Set).StringVar(&c.content.Value)
	c.CmdClause.Flag("dynamic", "Whether the VCL snippet is dynamic or versioned").Action(c.dynamic.Set).BoolVar(&c.dynamic.Value)
	c.CmdClause.Flag("name", "The name of the VCL snippet to update").StringVar(&c.name)
	c.CmdClause.Flag("new-name", "New name for the VCL snippet").Action(c.newName.Set).StringVar(&c.newName.Value)
	c.CmdClause.Flag("priority", "Priority determines execution order. Lower numbers execute first").Short('p').Action(c.priority.Set).IntVar(&c.priority.Value)
	c.RegisterFlag(cmd.StringFlagOpts{
		Name:        cmd.FlagServiceIDName,
		Description: cmd.FlagServiceIDDesc,
		Dst:         &c.manifest.Flag.ServiceID,
		Short:       's',
	})
	c.RegisterFlag(cmd.StringFlagOpts{
		Action:      c.serviceName.Set,
		Name:        cmd.FlagServiceName,
		Description: cmd.FlagServiceDesc,
		Dst:         &c.serviceName.Value,
	})
	c.CmdClause.Flag("snippet-id", "Alphanumeric string identifying a VCL Snippet").StringVar(&c.snippetID)

	// NOTE: Locations is defined in the same snippet package inside create.go
	c.CmdClause.Flag("type", "The location in generated VCL where the snippet should be placed").HintOptions(Locations...).Action(c.location.Set).EnumVar(&c.location.Value, Locations...)

	return &c
}

// UpdateCommand calls the Fastly API to update an appropriate resource.
type UpdateCommand struct {
	cmd.Base

	autoClone      cmd.OptionalAutoClone
	content        cmd.OptionalString
	dynamic        cmd.OptionalBool
	location       cmd.OptionalString
	manifest       manifest.Data
	name           string
	newName        cmd.OptionalString
	priority       cmd.OptionalInt
	serviceName    cmd.OptionalServiceNameID
	serviceVersion cmd.OptionalServiceVersion
	snippetID      string
}

// Exec invokes the application logic for the command.
func (c *UpdateCommand) Exec(in io.Reader, out io.Writer) error {
	serviceID, serviceVersion, err := cmd.ServiceDetails(cmd.ServiceDetailsOpts{
		AutoCloneFlag:      c.autoClone,
		APIClient:          c.Globals.APIClient,
		Manifest:           c.manifest,
		Out:                out,
		ServiceNameFlag:    c.serviceName,
		ServiceVersionFlag: c.serviceVersion,
		VerboseMode:        c.Globals.Flag.Verbose,
	})
	if err != nil {
		c.Globals.ErrLog.AddWithContext(err, map[string]interface{}{
			"Service ID":      serviceID,
			"Service Version": errors.ServiceVersion(serviceVersion),
		})
		return err
	}

	if c.dynamic.WasSet {
		input, err := c.constructDynamicInput(serviceID, serviceVersion.Number)
		if err != nil {
			c.Globals.ErrLog.AddWithContext(err, map[string]interface{}{
				"Service ID":      serviceID,
				"Service Version": serviceVersion.Number,
			})
			return err
		}
		v, err := c.Globals.APIClient.UpdateDynamicSnippet(input)
		if err != nil {
			c.Globals.ErrLog.AddWithContext(err, map[string]interface{}{
				"Service ID":      serviceID,
				"Service Version": serviceVersion.Number,
			})
			return err
		}
		text.Success(out, "Updated dynamic VCL snippet '%s' (service: %s)", v.ID, v.ServiceID)
		return nil
	}

	input, err := c.constructInput(serviceID, serviceVersion.Number)
	if err != nil {
		c.Globals.ErrLog.AddWithContext(err, map[string]interface{}{
			"Service ID":      serviceID,
			"Service Version": serviceVersion.Number,
		})
		return err
	}
	v, err := c.Globals.APIClient.UpdateSnippet(input)
	if err != nil {
		c.Globals.ErrLog.AddWithContext(err, map[string]interface{}{
			"Service ID":      serviceID,
			"Service Version": serviceVersion.Number,
		})
		return err
	}
	text.Success(out, "Updated VCL snippet '%s' (previously: '%s', service: %s, version: %d, type: %v, priority: %d)", v.Name, input.Name, v.ServiceID, v.ServiceVersion, v.Type, v.Priority)
	return nil
}

// constructDynamicInput transforms values parsed from CLI flags into an object to be used by the API client library.
func (c *UpdateCommand) constructDynamicInput(serviceID string, serviceVersion int) (*fastly.UpdateDynamicSnippetInput, error) {
	var input fastly.UpdateDynamicSnippetInput

	input.ID = c.snippetID
	input.ServiceID = serviceID

	if c.newName.WasSet {
		return nil, fmt.Errorf("error parsing arguments: --new-name is not supported when updating a dynamic VCL snippet")
	}

	if c.snippetID == "" {
		return nil, fmt.Errorf("error parsing arguments: must provide --snippet-id to update a dynamic VCL snippet")
	}
	if c.content.WasSet {
		input.Content = fastly.String(cmd.Content(c.content.Value))
	}

	return &input, nil
}

// constructInput transforms values parsed from CLI flags into an object to be used by the API client library.
func (c *UpdateCommand) constructInput(serviceID string, serviceVersion int) (*fastly.UpdateSnippetInput, error) {
	var input fastly.UpdateSnippetInput

	input.Name = c.name
	input.ServiceID = serviceID
	input.ServiceVersion = serviceVersion

	if c.snippetID != "" {
		return nil, fmt.Errorf("error parsing arguments: --snippet-id is not supported when updating a versioned VCL snippet")
	}
	if c.name == "" {
		return nil, fmt.Errorf("error parsing arguments: must provide --name to update a versioned VCL snippet")
	}
	if c.newName.WasSet {
		input.NewName = fastly.String(c.newName.Value)
	}
	if c.priority.WasSet {
		input.Priority = fastly.Int(c.priority.Value)
	}
	if c.content.WasSet {
		input.Content = fastly.String(cmd.Content(c.content.Value))
	}
	if c.location.WasSet {
		location := fastly.SnippetType(c.location.Value)
		input.Type = &location
	}

	return &input, nil
}
