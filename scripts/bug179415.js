// mmcollect -u xxxx -h xxxx -s bug179415.js "show ip interface brief; show datapath session table"

// "data0" espera el resultado de "show ip interface brief"
data0 = data0.data || data0;

// Obtengo los prefijos de todas las interfaces de la controladora
var interfaces = new Array();
var redes = new Array();
for (var i = 0; i < data0.length; i++) {
	var line = data0[i]
	if (line.indexOf("vlan") >= 0) {
		// El tercer campo es la IP de la interfaz
		var ip = line.match(/\S+/g)[2];
		var red = ip.substr(0, ip.lastIndexOf(".") + 1);
		// safety check
		if (red.length > 8) {
			interfaces.push(ip);
			redes.push(red);
		}
	}
}

// Devuelve true si la IP esta en la red
function enRed(ip, redes) {
	for (var i = 0; i < redes.length; i++) {
		var red = redes[i];
		if (ip.indexOf(red) == 0) return true;
	}
	return false;
}

// data1 espera el resultado de "show datapath session table"
data1 = data1.data || data1;
var corrupt = new Array();
var users = {};
for (var i = 0; i < data1.length; i++) {
	var line = data1[i]
	if (line.indexOf("nh 0x") >= 0) {
		// primer y segundo campo son IP origen y destino
		var partes = line.match(/\S+/g);
		origen = partes[0];
		destino = partes[1];
		// Es una entrada corrupta si el origen y el destino son redes locales
		if (enRed(origen, redes) && enRed(destino, redes)) {
			corrupt.push(line);
			// No tengo en cuenta las IPs de la propia controladora
			if (!enRed(origen, interfaces)) {
				users[origen] = 1;
			}
			if (!enRed(destino, interfaces)) {
				users[destino] = 1;
			}
		}
	}
}

// Manda el user delete a la controladora
for (var key in users) {
	if (users.hasOwnProperty(key)) {
		var result = Post("/md", "object/aaa_user_delete", { "ipaddr": key });
		corrupt.push("SENT aaa_user_delete FOR "+key+": "+result);
	}
}

// El resultado del script son las entradas corruptas
corrupt;
