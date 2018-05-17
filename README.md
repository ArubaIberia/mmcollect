# mmcollect

MMCollect is a small tool to collect the output of "show" commands from all controllers (also called MDs or Managed Devices) connected to a Mobility Manager.

The basic usage requires just the IP address (or hostname) of the Mobility Manager, an username, and the command to run: 

```bash
# Will dump the "show version" command of all MDs managed by the MM
mmcollect -h your.mm.ip.address -u username "show version"
```

More advanced use cases are described below.

## Connecting to the MM and Controllers

MMCollect does not use a regular telnet/ssh connection to run the show commands, but the REST API os AOS 8.X. To connect to the API, the host where you run mmconnect needs connectivity to **TCP PORT 4343** of the MM and controllers.

A side effect of using the API is that show commands must be typed full, with no abbreviations. I.e. `show ip int brief` won't work, you need to type the whole thing: `show ip interface brief`

Another side effect is that pipes are not supported, i.e. `show ip interface brief | include vlan` won't work either. Filtering can be done using [jsonpath](https://github.com/oliveagle/jsonpath) expressions, see the filtering section below for some examples.

If you are not sure of the full syntax of a command and want to check, you can limit the number of controllers to target, so you don't fire a wrong command to hundreds of MDs:

```bash
# -l option limits the number of controllers to connect to
mmcollect -h your.mm.ip.address -u username -l 1 "show ip interfaces brief"
# You will get an error because it is not "show ip interfaces", but "show ip interface". Try again
mmcollect -h your.mm.ip.address -u username -l 1 "show ip interface brief"
# Now that you tested it works, you can remove the -l 1
mmcollect -h your.mm.ip.address -u username "show ip interface brief"
```

You can also run several consecutive commands, separated by a semicolon:

```bash
mmcollect -h your.mm.ip.address -u username "show ip interface brief; show user-table verbose"
```

## Running several threads in parallel

Just add the *-t threads* option to the command line to set the number of parallel jobs. By default, it is 25.

```bash
mmcollect -h your.mm.ip.address -u username -t 50 "show ip interface brief; show user-table verbose"
```

## Selecting the controllers to collect data

By default mmcollect runs the commands you provide in every controller that is "up", from the point of view of the MM. But you can narrow down on which controllers will the command be run, by specifyng a [jsonpath](https://github.com/oliveagle/jsonpath) filter with the *-f <filter>* command line option.

A few examples:

```bash
# Run the commands on Aruba 7005 controllers:
mmcollect -h your.mm.ip.address -u username -f "?(@.Model == 'Aruba7005')" "show version"

# Run the commands on controllers with SNMP Location matching regexp "Building1"
mmcollect -h your.mm.ip.address -u username -f "?(@.Location =~ /Building1/)" "show version"

# Run the command on controllers with status "UPDATE SUCCESSFUL"
mmcollect -h your.mm.ip.address -u username -f "?(@.Configuration_State == 'UPDATE SUCCESSFUL')" "show version"
```

Notice from the last example above how mmcollect replaces whitespace and other non-alphanumeric characters with underscores ('_') in attribute names, to workaround some problems with that kind of attributes in jsonpath expressions.

You can specify several filter criteria, separated by pipes (**"|"**), for example:

```bash
# Run the commands on Aruba 7010 controllers with status "UPDATE SUCCESSFUL":
mmcollect -h your.mm.ip.address -u username -f "?(@.Model == 'Aruba7010') | ?(@.Configuration_State == 'UPDATE SUCCESSFUL')" "show version"
```

Operators supported (referenced from github.com/jayway/JsonPath):

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

## Filtering the output of a command

The output of "show" requests sent through the REST API is not raw text, but json objects. You will notice if you try something like `show ip access-list brief`:

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

You see, you get a JSON object as a reply! mmcollect supports [jsonpath](https://github.com/oliveagle/jsonpath) syntax to filter the value returned by the controller, too. Say you want to collect only session access-list:

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
  "Use Count": null
}
{
  "Name": "allow-printservices",
  "Roles": null,
  "Type": "session(4)",
  "Use Count": null
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
  "Use Count": null
}
```

You can also concatenate several filters, separated by pipes. **If the top-level object you are filtering is an array, skip the "$[ ]" part**. I.e. **Instead of**:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')] | $[?(@.Name =~ /print/)]"
```

**Do**:

```bash
# Notice how we skip the starting "$[" and ending "]" in the second filter,
# because the first filter returns an array.
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6[?(@.Type == 'session(4)')] | ?(@.Name =~ /print/)"
```

You can even do:

```bash
mmcollect -u admin -h your.mm.ip.address "show ip access-list brief | $.Access_list_table_4_IPv4_6_IPv6 | ?(@.Type == 'session(4)') | ?(@.Name =~ /print/)"
```

## Flatenning a JSON object

Sometimes you don't want the full JSON object returned by the controller, but just a few fields. MMcollect lets you combine filtering with **sttribute selection**, usign the **>** sign after the command or filter. Name the fields you want extracted, separated by commas:

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

## Delay between commands

What if you want to run some command a few times, like "show datapath session table", waiting a few seconds between each run? mmcollect got you covered with the flag *-d delay_seconds*:

```bash
# Run "show datapath session" twice, with 5 seconds delay between each run
mmcollect -u admin -h your.mm.ip.address -d 5 "show datapath session table | $.data; show datapath session table | $.data"
```

## Saving output to files

Finally, you can tell mmconnect to save the output of each controller to a separate file with the *-o <prefix>* flag. Each controller will get its outout saved to a separate file, named after the controller's IP address.

For instance, if you want to same all the logs into folder *logs* with names *switch-<IP.address.of.controller>.log*, give mmcollect the prefix *-t logs/switch-*:

```bash
# Run "show datapath session" three times, with 5 seconds delay between each run
mmcollect -u admin -h your.mm.ip.address -o logs/switch- "show datapath session table"
```
