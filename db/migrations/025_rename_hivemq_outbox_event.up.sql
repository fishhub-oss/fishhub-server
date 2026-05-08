UPDATE outbox_events
SET event_type = 'mqtt.provision_device'
WHERE event_type = 'hivemq.provision_device'
  AND status != 'completed';
