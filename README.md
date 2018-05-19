# mmcollect

mmcollect is a small tool to collect the output of "show" commands from all controllers (also called MDs or Managed Devices) connected to a Mobility Manager.

The basic usage requires just the IP address (or hostname) of the Mobility Manager, an username, and the command to run: 

```bash
# Will dump the "show version" command of all MDs managed by the MM
mmcollect -h your.mm.ip.address -u username "show version"
```

More advanced use cases are described below.

## Connecting to the MM and Controllers

mmcollect does not use a regular telnet/ssh connection to run the show commands, but the REST API of AOS 8.X. To connect to the API, the host where you run mmcollect needs connectivity to **TCP PORT 4343** of the MM and controllers.

A side effect of using the API is that show commands must be typed full, with no abbreviations. I.e. `show ip int brief` won't work, you need to type the whole thing: `show ip interface brief`

Another side effect is that filters behave a little different, i.e. `show ip interface brief | include vlan` does not do what you would expect. Filtering should be done using [jsonpath](https://github.com/oliveagle/jsonpath) expressions, see the filtering section below for some examples.

## Filtering

The output of "show" requests received through the REST API is not raw text, but json objects. You will notice if you try something like `show ip access-list brief`:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief"
Password: 

2018/05/10 19:10:06 Getting the switch list
2018/05/10 19:10:07 Switch list collected, working on a set of 2
2018/05/10 19:10:07 Waiting for workers to complete!
CONTROLLER  x.x.x.x
{
  "Access_list_table_4_IPv4_6_IPv6": [
    {
      "Name": "allow-diskservices",
      "Roles": null,
      "Type": "session(4)",
      "Use_Count": null
    },
    {
      "Name": "allow-printservices",
      "Roles": null,
      "Type": "session(4)",
      "Use_Count": null
    },
# ... omitted for brevity
```

You see, you get a JSON object as a reply! Notice from the example above how mmcollect replaces whitespace and other non-alphanumeric characters with underscores ('_') in attribute names, to workaround some problems when working with those attributes.

mmcollect supports [jsonpath](https://github.com/oliveagle/jsonpath) syntax to filter the values returned by the controller. Operators supported (referenced from github.com/jayway/JsonPath):

| Operator | Supported | Description |
| ---- | :---: | ---------- |
| $ 					  | Y | The root element to query. This starts all path expressions. |
| @ 				      | Y | The current node being processed by a filter predicate. |
| * 					  | X | Wildcard. Available anywhere a name or numeric are required. |
| .. 					  | X | Deep scan. Available anywhere a name is required. |
| .<name> 				  | Y | Dot-notated child |
| ['<name>' (, '<name>')] | X | Bracket-notated child or children |
| [<number> (, <number>)] | Y | Array index or indexes |
| [start:end] 			  | Y | Array slice operator |
| [?(<expression>)] 	  | Y | Filter expression. Expression must evaluate to a boolean value. |

For example, say you want to collect only session access-list:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')]"
Password: 

2018/05/10 19:15:41 Getting the switch list
2018/05/10 19:15:41 Switch list collected, working on a set of 2
2018/05/10 19:15:41 Waiting for workers to complete!
CONTROLLER  x.x.x.x
{
  "Name": "allow-diskservices",
  "Roles": null,
  "Type": "session(4)",
  "Use_Count": null
}
{
  "Name": "allow-printservices",
  "Roles": null,
  "Type": "session(4)",
  "Use_Count": null
}
# ... omitted for brevity
```

Or you want only ACLs with the name "print" in it:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Name =~ /print/)]"
Password: 

2018/05/10 19:15:41 Getting the switch list
2018/05/10 19:15:41 Switch list collected, working on a set of 2
2018/05/10 19:15:41 Waiting for workers to complete!
CONTROLLER  x.x.x.x
{
  "Name": "allow-printservices",
  "Roles": null,
  "Type": "session(4)",
  "Use_Count": null
}
```

### Concatenating filters

You can concatenate several filters, separated by pipes. The output of one filter is fed into the next one. **If the output of some filter is an array, you must skip the "$[ ]" part in the next filter**. I.e. **Instead of**:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')] | $[?(@.Name =~ /print/)]"
```

**Do**:

```bash
# Since the output of first filter is an array, we skip the starting "$[" and ending "]" in the second filter
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')] | ?(@.Name =~ /print/)"
```

You can even do:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6 | ?(@.Type == 'session(4)') | ?(@.Name =~ /print/)"
```

### Plain text filters

You can also use plain old **include** or **begin** keywords to filter strings or arrays of strings. If the output of one filter is a string or list of strings, the next filter can be an *include* or *begin*:

```bash
mmcollect -u admin -h your.mm.ip.address "show datapath session table | $._data | inc 10.1.2.3"
```

## Selecting controllers

By default mmcollect selects the controllers to scan by running `show switches` in the Mobility Manager, and filtering the output with the filter expression `?(@.Status == 'up')`) to find out all controllers that are 'up'.

You can narrow down the controllers that mmcollect will scan by specifyng additional [jsonpath](https://github.com/oliveagle/jsonpath) filter expressions with the *-f <filter>* flag. A few examples:

```bash
# Run the commands on Aruba 7005 controllers:
mmcollect -h your.mm.ip.address -u username -f "?(@.Model == 'Aruba7005')" "show version"

# Run the commands on controllers with SNMP Location matching regexp "Building1"
mmcollect -h your.mm.ip.address -u username -f "?(@.Location =~ /Building1/)" "show version"

# Run the command on controllers with status "UPDATE SUCCESSFUL"
mmcollect -h your.mm.ip.address -u username -f "?(@.Configuration_State == 'UPDATE SUCCESSFUL')" "show version"
```

You can also specify several filter criteria separated by pipes, as usual:

```bash
# Run the commands on Aruba 7010 controllers with status "UPDATE SUCCESSFUL":
mmcollect -h your.mm.ip.address -u username -f "?(@.Model == 'Aruba7010') | ?(@.Configuration_State == 'UPDATE SUCCESSFUL')" "show version"
```

## Field selectors

Sometimes you don't want the full JSON object returned by the controller, but just a few fields. MMcollect lets you combine filtering with **field selection**, usign the **>** sign after the command or filter. Name the fields you want extracted, separated by commas:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')] > Name, Type"
Password: 

2018/05/10 19:15:41 Getting the switch list
2018/05/10 19:15:41 Switch list collected, working on a set of 2
2018/05/10 19:15:41 Waiting for workers to complete!
CONTROLLER  x.x.x.x
allow-diskservices;session(4)
allow-printservices;session(4)
ap-acl;session(4)
captiveportal;session(4)
citrix-acl;session(4)
# ... omitted for brevity
```

## Running several commands in a row

You can run several consecutive commands, separated by a semicolon:

```bash
mmcollect -h your.mm.ip.address -u username "show ip interface brief; show user-table verbose"
```

Each command can have its own set of filters and field selectors.

```bash
mmcollect -h your.mm.ip.address -u username "show ip interface brief | inc vlan; show user-table verbose"
```

## Running several threads in parallel

Just add the *-t threads* option to the command line to set the number of parallel jobs. By default, it is 25.

```bash
mmcollect -h your.mm.ip.address -u username -t 50 "show ip interface brief; show user-table verbose"
```

## Delay between commands

What if you want to run some command a few times, like "show datapath session table", waiting a few seconds between each run? mmcollect got you covered with the flag *-d delay_seconds*:

```bash
# Run "show datapath session" twice, with 5 seconds delay between each run
mmcollect -u admin -h your.mm.ip.address -d 5 "show datapath session table | $.data; show datapath session table | $.data"
```

## Saving output to files

You can tell mmconnect to save the output of each controller to a separate file with the *-o <prefix>* flag. Each controller will get its output saved to a separate file, named after the controller's IP address.

For instance, if you want to same all the logs into folder *logs* with names *switch-<IP.address.of.controller>.log*, give mmcollect the prefix *-t logs/switch-*:

```bash
# Run "show datapath session" three times, with 5 seconds delay between each run
mmcollect -u admin -h your.mm.ip.address -o logs/switch- "show datapath session table"
```

## Running in batch

If you want to run the command in batch mode (not interactively), you can provide the password through the *-p <password> flag, for example:

```bash
mmcollect -u admin -h your.mm.ip.address -p <your-password> "show datapath session table"
```

It may be wise to use environment variables or shell expansion instead of a literal password, so your credentials do not show up in the output of "ps -a", for example:

```bash
# Use an environment variable
mmcollect -u admin -h your.mm.ip.address -p "$MYPASS" "show datapath session table"
# Use a secret file
mmcollect -u admin -h your.mm.ip.address -p `cat ~/.secret_pass` "show datapath session table"
```

## Scripting

mmcollect can run a script once per controller. Set the path of the script with the *-s <filename>* flag, and mmcollect will read the file and run it after it finishes collecting the data of each controller.

The script must be valid JavaScript, and is parsed using the [otto](https://github.com/robertkrimen/otto) engine. The javascript code will have access to the following global variables and functions:

- `data: Array`: An array with the output of each command: data[0] is the result of the first `show` command, data[1] is the result of the second, and so on.
- `getenv(name: string)`: A function to get environment variables by name.
- `session: Object`: A session object that lets the script interact with the controller. It currently supports:

  - `date: string`: The date of the session, in `YYYYMMdd` format.
  - `ip: string`: The IP address of the controller (read-only).
  - `post(cfg_path: string, api_endpoint: string, data: object)`: Send HTTP POST requests to the controller. Returns `null` on success, an error object otherwise.

For instance, say you want to drop all users sending SMB traffic, using `aaa user delete`. You can look for port 445 in the output of the `show datapath session table`, and POST a message to the controller to delete those users. Save this script as *aaa_user_delete.js*:

```js
// The script expects data0 to be the output of "show datapath session table | $._data | inc 445"
_.each(data[0], function(line) {
  // The first field in the output of "show datapath session table" is the source IP.
  // The API call to delete an IP address is "/configurations/object/aaa_user_delete;
  // you can skip the "/configurations/", it is added by the `Post` function.
  var source_ip = line.match(/\S+/g)[0];
  session.post("/mm", "object/aaa_user_delete", { "ipaddr": source_ip });
})
```

And then run your collector with *-s aaa_user_delete.js*:

```bash
mmcollect -u admin -h your.mm.ip.address -s aaa_user_delete.js "show datapath session table | $._data | include 445"
```
