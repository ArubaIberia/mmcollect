// mmcollect -u xxxx -h xxxx -s bug179415.js \
//     "show ip interface brief | $._data | inc 'vlan ';" \
//     "show datapath session table | $._data | inc 'nh 0x'"
//
// "data[0]" contiene el resultado de "show ip interface brief | $._data | include 'vlan '"
// "data[1]" contiene el resultado de "show datapath session table | $._data | inc 'nh 0x'"

// Obtengo las direcciones IP de las interfaces
var interfaces = _.reduce(data[0], function (memo, line) {
	// El tercer campo de cada línea debería ser la dirección IP de una interfaz
	var ip = line.match(/\S+/g)[2];
	// Compruebo que parece una IP
	if (/^[0-9\.]{7,}$/.test(ip)) {
		memo.push(ip);
	}
	return memo;
}, []);
var redes = _.map(interfaces, function(ip) {
	// Simplificando, no tengo en cuenta la mascara
	return ip.substr(0, ip.lastIndexOf(".") + 1);
});

// Devuelve las IPs afectadas por el bug
function ips_afectadas(ips, interfaces, redes) {
	// Si tanto la IP origen como destino son locales, el flujo está afectado.
	if (_.every(ips, function(ip) { return en_red(ip, redes); })) {
		// Devuelvo las IPs que pertenezcan a la controladora.
		return _.filter(ips, function(ip) { return _.indexOf(interfaces, ip) < 0 });
	}
}

// True si la IP dada pertenece a alguna de las redes
function en_red(ip, redes) {
	return _.some(redes, function(red) { return ip.indexOf(red) == 0; });
}

// Obtengo la lista de entradas afectadas por el problema en esta controladora
var users = {};
var result = _.filter(data[1], function(line) {
	// primer y segundo campo son IP origen y destino
	var ips = ips_afectadas(line.match(/\S+/g).slice(0, 2), interfaces, redes);
	if (ips !== undefined) {
		_.each(ips, function (ip) { users[ip] = 1; });
		return true;
	}
});

// Manda el user delete a la controladora, y añade el resultado a la salida
result.concat(_.map(_.keys(users), function(ip) {
	return "SENT aaa_user_delete FOR " + ip + ": " + session.post("/mm", "object/aaa_user_delete", { "ipaddr": ip });
}));
