import sys
import json
import jinja2

input_path = sys.argv[1]
output_path = sys.argv[2]

with open(input_path, "r") as f:
    data = json.load(f)


def split_command(cmd: str) -> list[str]:
    return cmd.split("/")


forbidden_commands = ["type", "interface", "import", "default", "select", "package"]

dataDict = {}
for cmd in data:
    result = cmd["result"]
    result["is_cmd"] = (len(result["subcmds"]) == 0)
    result["subcmds"] = [subcmd.replace("-", "__") for subcmd in result["subcmds"] if subcmd not in forbidden_commands]
    result["args"] = [arg.replace(".", "dot_") for arg in result["args"] if arg not in forbidden_commands]
    result["cmd_path"] = cmd["cmd"]
    dataDict[cmd["cmd"].replace("-", "__").replace("/", "_")] = result

# TODO builder style pattern for command arguments

env = jinja2.Environment(
    loader=jinja2.DictLoader({
        "fluent.go": """package fluent

{% if not result['is_cmd'] -%}  
import (
    "github.com/jenrik/go-routeros-client/clients"
)
{% endif %}

type root{{ cmd }} struct {
    client *Client
}
{% for subcmd in result['subcmds'] -%}
{% if data[cmd + '_' + subcmd]['is_cmd'] -%}
{% with subcmd_result=data[cmd + '_' + subcmd] -%}
func (fluent root{{ cmd }}) cmd_{{ subcmd }}({% if subcmd_result["args"]|length > 0 %}
{% for arg in subcmd_result["args"] %}    arg_{{ arg.replace('-', '_') }} string,
{% endfor %}{% endif %}) (error, clients.Response) {
    args := map[string]string{}
    
    {% for arg in subcmd_result["args"] %}if arg_{{ arg.replace('-', '_') }} != "" {
        args["{{ arg }}"] = arg_{{ arg.replace('-', '_') }}
    }
    {% endfor %}    
    return fluent.client.client.SendCommand("{{ subcmd_result['cmd_path'] }}", args)
}{% endwith %}{% else -%}
func (fluent root{{ cmd }}) cat_{{ subcmd }}() root{{ cmd }}_{{ subcmd }} {
    return root{{ cmd }}_{{ subcmd }}{
        client: fluent.client,
    }
}
{% endif %}
{% endfor %}"""
    })
)

for cmd, result in dataDict.items():
    if result["is_cmd"]:
        continue

    stream = env.get_template("fluent.go").stream(data=dataDict, cmd=cmd, result=result)
    with open(output_path + "fluent_root" + cmd + ".go", "w") as f:
        f.truncate()
        stream.dump(f)
