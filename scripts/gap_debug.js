// Looks for entries in "show gap-debug" with LMS-Cookie == 0.0.0.0
// Matches against "up" controllers and "down" APs, to find any "up" controller
// whose APs don't show "up" in the MM.
//
// Command:
//
// mmcollect -u <your user> -h <your host> -s gap_debug.js -f "?(@.Type == 'master')" \
//   "show gap-debug  | $.GAP_Master_LMS_Table | ?(@.LMS_Cookie == '0.0.0.0,00000000') > IP;" \
//   "show switches debug | $.All_Switches | ?(@.Status == 'up') > IP_Address, Name;" \
//   "show ap database | $.AP_Database > Switch_IP, Status;"
//
// Single line:
//
// "show gap-debug  | $.GAP_Master_LMS_Table | ?(@.LMS_Cookie == '0.0.0.0,00000000') > IP; show switches debug | $.All_Switches | ?(@.Status == 'up') > IP_Address, Name; show ap database | $.AP_Database > Switch_IP, Status;"

// null_cookie to contain IP addresses of all switches with LMS Cookie = 0.
// data[0] contains "show gap-debug | $.GAP_Master_LMS_Table | ?(@.LMS_Cookie == '0.0.0.0,00000000') > IP;"
var null_cookie = data[0];
null_cookie.sort();
//console.log("NULL_COOKIE: " + null_cookie);

// switch_map to contain map from switch name to IP address, for all up switches.
// data[1] contains "show switches debug | $.All_Switches | ?(@.Status == 'up') > IP_Address, Name;"
var switch_map = _.reduce(data[1], function(memo, line) {
    var parts = line.split(";");
    var ip = parts[0];
    var name = parts[1];
    memo[ip] = name + " (" + ip + ")";
    return memo;
}, {});
var switch_up = _.keys(switch_map);
switch_up.sort();
//console.log("SWITCH_UP: " + switch_up);

// switch_down to contain IP addresses of all switches that don't have APs up.
// data[2] contains "show ap database | $.AP_Database > Switch_IP, Status"
var ap_count = _.reduce(data[2], function(memo, line) {
    var parts = line.split(";");
    var ip = parts[0];
    var status = parts[1];
    var count = memo[ip];
    if (!count) {
        count = { up: 0, down: 0 };
    }
    if (status == 'Down') {
        count.down += 1;
    } else {
        count.up += 1;
    }
    memo[ip] = count;
    return memo;
}, {});
var ap_down = _.filter(_.keys(ap_count), function(key) {
    var count = ap_count[key];
    return (count.up <= 0 && count.down > 0);
});
ap_down.sort();
//console.log("AP_DOWN: " + ap_down);

// Final result: All up controllers with LSM cookie 0.0.0.0, and no APs up
// Note: Otto's pick does not support specifiyng a function as attrib filter
var errored = _.filter(switch_up, function(ip) {
    return _.indexOf(null_cookie, ip, true) >= 0 && _.indexOf(ap_down, ip, true) >= 0;
});
_.map(errored, function(ip) { return switch_map[ip]; });
