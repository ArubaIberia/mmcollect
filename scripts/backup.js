// mmcollect -u xxxx -h xxxx -f "?(@.Model == 'ArubaMM-VA')" -s backup.js "show version"

// Comprueba si el resultado del POST es un error
function errMsg(result) {
	// Compruebo que la respuesta viene en el formato adecuado
	if (!result._global_result || !result._global_result.status_str) {
		return JSON.stringify(result)
	}
	// y que tiene un mensaje de success
	if (result._global_result.status_str.toLowerCase().indexOf("success") < 0) {
		return result._global_result._status_str;
	}
	return null;
}

// Hace un backup de la flash a un servidor stp
function backup(scphost, scpuser, scppass, scppath) {
	var backup_data = {
		"backup_flash": "flash",
		"filename": "flashbackup"
	};
	var msg = errMsg(session.post("/md", "object/flash_backup", backup_data));
	if (msg !== null) {
		return "No se pudo hacer el backup: " + msg;
	}
	if (scppath.length > 0 && scppath[scppath.length-1] != "/") {
		scppath = scppath + "/";
	}
	var scp_data = {
		"scphost": scphost,
		"destfilename": scppath + "/flashbackup-" + session.ip + "-" + session.date + ".tgz",
		"srcfilename": backup_data["filename"] + ".tar.gz",
		"username": scpuser,
		"passwd": scppass
	};
	msg = errMsg(session.post("/md", "object/copy_flash_scp", scp_data));
	if (msg !== null) {
		return "No se pudo copiar el backup a " + scphost + " (usuario " + scpuser + "): " + msg;
	}
	return "Backup de " + session.ip + " en " + scphost + ":" + scppath + " finalizado";
}

// Datos de configuracion
function main() {
	// Compruebo si están todas las variables definidas
	var missing = _.filter(["SCP_HOST", "SCP_PATH", "SCP_USER", "SCP_PASS"], function (env) {
		return getenv(env) == "";
	})
	if (missing.length > 0) {
		return _.map(missing, function(env) {
			return "ERROR: Missing environment variable " + env;
		})
	}
	// Hago el backup
	var scphost = getenv("SCP_HOST"); // Must be IP address. Hostname not supported.
	var scppath = getenv("SCP_PATH");
	var scpuser = getenv("SCP_USER");
	var scppass = getenv("SCP_PASS");
	return backup(scphost, scpuser, scppass, scppath);
}
main();
