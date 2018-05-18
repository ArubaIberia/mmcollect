// mmcollect -u xxxx -h xxxx -s bug179415.js "show ip interface brief; show datapath session table"

// "data0" espera el resultado de "show ip interface brief"
data0 = data0._data || data0;

// Obtengo las direcciones IP de todas las interfaces de la controladora
var interfaces = _.reduce(data0, function (memo, line) {
	if (line.indexOf("vlan ") >= 0) {
		// El tercer campo de esta línea debería ser la dirección IP de la interfaz
		var ip = line.match(/\S+/g)[2];
		// Compruebo que parece una IP
		if (/^[0-9\.]{7,}$/.test(ip)) {
			memo.push(ip);
		}
	}
	return memo;
}, []);
var redes = _.map(interfaces, function(ip) {
	// Simplificando, no tengo en cuenta la mascara
	return ip.substr(0, ip.lastIndexOf(".") + 1);
});

// Devuelve true si la IP esta en la red
function enRed(ip, redes) {
	return _.some(redes, function(red) { return ip.indexOf(red) == 0; })
}

// "data1" espera el resultado de "show datapath session table"
data1 = data1._data || data1;

// Obtengo la lista de entradas e IPs afectadas por el problema en esta controladora
users = {};
corrupt = _.filter(data1, function(line) {
	if (line.indexOf("nh 0x") > 0) {
		// primer y segundo campo son IP origen y destino
		var ips = line.match(/\S+/g).slice(0, 2);
		// Es una entrada corrupta si el origen y el destino son redes locales
		if (_.every(ips, function (ip) { return enRed(ip, redes); })) {
			_.each(ips, function(ip) { users[ip] = 1; });
			return true;
		}
		return false;
	}
});

// Manda el user delete a la controladora
corrupt.concat(_.map(_.keys(users), function(ip) {
	return "SENT aaa_user_delete FOR " + ip + ": " + Post("/md", "object/aaa_user_delete", { "ipaddr": ip });
}));
