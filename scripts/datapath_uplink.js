// mmcollect -u xxxx -h xxxx -T 1800 -f "?(@.Model =~ /7010/) | ?(@.Version =~ /8.5.0/)" -s datapath_uplink.js \
//     "show ip interface brief | $._data | inc 'vlan ';" \
//     "show datapath session uplink | $._data | begin SIDX"

// Detecta si hay tráfico local que esté siendo redirigido a un uplink

// "data[0]" contiene el resultado de "show ip interface brief | $._data | include 'vlan '"
// Obtengo las direcciones IP de las interfaces
var interfaces = _.reduce(data[0], function (memo, line) {
	// El tercer campo de cada linea deberia ser la direccion IP de una interfaz
	var fields = line.match(/\S+/g);
	var vlan = fields[1];
	var ip = fields[2];
	// Compruebo que parece una IP
	if (/^[0-9\.]{7,}$/.test(ip)) {
        // Las direcciones de uplink no me interesan
		if (vlan != "3" && vlan != "4094") {
			memo.push(ip);
		}
	}
	return memo;
}, []);
var redes = _.map(interfaces, function(ip) {
	// Simplificando, no tengo en cuenta la mascara
	return ip.substr(0, ip.lastIndexOf(".") + 1);
});

// Comprueba si las dos IPs (origen y destino) son internas a la tienda.
// Si lo son, las devuelve.
function ips_internas(ips, interfaces, redes) {
	// Si tanto la IP origen como destino son locales, el flujo esta afectado.
	if (_.every(ips, function(ip) { return en_red(ip, redes); })) {
		// Devuelvo las IPs que pertenezcan a la controladora.
		return ips;
	}
}

// True si la IP dada pertenece a alguna de las redes
function en_red(ip, redes) {
	return _.some(redes, function(red) { return ip.indexOf(red) == 0; });
}

// "data[1]" contiene el resultado de "show datapath session uplink | $._data | begin SIDX"
// Obtengo la lista de sesiones afectadas por el problema en esta controladora
var total = 0;
var flows = _.filter(data[1], function(line) {
	// Contador para estar seguro de que se ejecuta el bucle
	total++;
	// primer y segundo campo son IP origen y destino
	var ips = ips_internas(line.match(/\S+/g).slice(0, 2), interfaces, redes);
	if (ips !== undefined) {
		return true;
	}
	return false;
});

console.log("REVISADOS " + total + " FLUJOS, ENCONTRADAS " + flows.length + " ANOMALIAS");

// Manda el user delete a la controladora, e interrumpe el bucle
if (flows.length > 0) {
	session.done();
	var msg = JSON.stringify(session.post("/mm", "object/tar_logs", { "tech-support": true }));
	flows.push("SENT tar logs tech FOR " + session.ip + ":" + msg);
}
flows;
