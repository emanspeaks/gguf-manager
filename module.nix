{ config, lib, pkgs, ... }:

let
  cfg = config.services.gguf-manager;
  configFile = pkgs.writeText "gguf-manager.json" (builtins.toJSON {
    modelsDir      = cfg.modelsDir;
    llamaServerURL = cfg.llamaServerURL;
    llamaService   = cfg.llamaService;
    port           = cfg.port;
    hfToken        = cfg.hfToken;
  });
in {
  options.services.gguf-manager = {
    enable = lib.mkEnableOption "gguf-manager local model manager UI";

    package = lib.mkOption {
      type        = lib.types.package;
      description = "The gguf-manager package to use.";
    };

    port = lib.mkOption {
      type    = lib.types.port;
      default = 9293;
      description = "Port the web UI listens on.";
    };

    modelsDir = lib.mkOption {
      type    = lib.types.str;
      default = "/var/lib/llama-models";
      description = "Path to the directory containing model subdirectories.";
    };

    llamaServerURL = lib.mkOption {
      type    = lib.types.str;
      default = "http://localhost:9292";
      description = "Base URL of the llama-server instance.";
    };

    llamaService = lib.mkOption {
      type    = lib.types.str;
      default = "llama-cpp.service";
      description = "systemd service name to restart after model changes.";
    };

    hfToken = lib.mkOption {
      type    = lib.types.str;
      default = "";
      description = "Optional HuggingFace token for private repos or higher rate limits.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.gguf-manager = {
      description = "gguf-manager — local GGUF model management UI";
      after       = [ "network.target" cfg.llamaService ];
      wantedBy    = [ "multi-user.target" ];

      path = [ pkgs.python3Packages.huggingface-hub ];

      serviceConfig = {
        ExecStart          = "${cfg.package}/bin/gguf-manager --config ${configFile}";
        User               = "llama-cpp";
        Group              = "llm";
        Restart            = "on-failure";
        RestartSec         = "5s";

        # Allow restarting llama-cpp.service via D-Bus
        AmbientCapabilities = "";
        # D-Bus policy must allow the llama-cpp user to manage units;
        # on NixOS this is typically handled by the polkit rule below.
      };
    };

    # Allow the llama-cpp user to restart the llama-cpp service without root.
    security.polkit.extraConfig = ''
      polkit.addRule(function(action, subject) {
        if (action.id == "org.freedesktop.systemd1.manage-units" &&
            action.lookup("unit") == "${cfg.llamaService}" &&
            subject.user == "llama-cpp") {
          return polkit.Result.YES;
        }
      });
    '';
  };
}
