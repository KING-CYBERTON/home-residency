import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:image_picker/image_picker.dart';

import '../models/inbox.dart';
import 'widgets.dart';

/// The vault accepts these four types (`UploadRequest.mime_type`).
const allowedMimeTypes = {
  'jpg': 'image/jpeg',
  'jpeg': 'image/jpeg',
  'png': 'image/png',
  'webp': 'image/webp',
  'pdf': 'application/pdf',
};

class PickedFile {
  const PickedFile(this.bytes, this.mimeType, this.name);
  final Uint8List bytes;
  final String mimeType;
  final String name;
}

/// Asks: take a photo, choose a photo, or choose a PDF. Photos are resized so uploads stay small
/// on metered data. Returns null if the person cancels.
Future<PickedFile?> pickDocumentFile(BuildContext context) async {
  final choice = await showModalBottomSheet<String>(
    context: context,
    builder: (c) => SafeArea(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (!kIsWeb)
            ListTile(
              leading: const Icon(Icons.photo_camera_outlined),
              title: const Text('Take a photo'),
              onTap: () => Navigator.pop(c, 'camera'),
            ),
          ListTile(
            leading: const Icon(Icons.photo_outlined),
            title: const Text('Choose a photo'),
            onTap: () => Navigator.pop(c, 'gallery'),
          ),
          ListTile(
            leading: const Icon(Icons.picture_as_pdf_outlined),
            title: const Text('Choose a PDF'),
            onTap: () => Navigator.pop(c, 'pdf'),
          ),
        ],
      ),
    ),
  );
  if (choice == null) return null;
  if (choice == 'pdf') {
    final file = await FilePicker.pickFile(type: FileType.custom, allowedExtensions: const ['pdf']);
    if (file == null) return null;
    return PickedFile(await file.readAsBytes(), 'application/pdf', file.name);
  }
  final x = await ImagePicker().pickImage(
    source: choice == 'camera' ? ImageSource.camera : ImageSource.gallery,
    maxWidth: 2000,
    maxHeight: 2000,
    imageQuality: 80,
  );
  if (x == null) return null;
  final ext = x.name.split('.').last.toLowerCase();
  final mime = x.mimeType ?? allowedMimeTypes[ext] ?? 'image/jpeg';
  if (!allowedMimeTypes.containsValue(mime)) {
    if (context.mounted) showMessage(context, 'Please use a JPG, PNG or PDF file.', error: true);
    return null;
  }
  return PickedFile(await x.readAsBytes(), mime, x.name);
}

/// Picks a file and stores it in the vault. Returns the new document, or null if cancelled.
Future<Document?> pickAndUploadDocument(
  BuildContext context, {
  required String documentType,
  DateTime? issuedOn,
  DateTime? expiresOn,
}) async {
  final api = context.api;
  final file = await pickDocumentFile(context);
  if (file == null) return null;
  if (file.bytes.length > 10 * 1024 * 1024) {
    if (context.mounted) showMessage(context, 'That file is over 10 MB. Please take a smaller photo.', error: true);
    return null;
  }
  return api.uploadDocument(
    documentType: documentType,
    mimeType: file.mimeType,
    bytes: file.bytes,
    filename: file.name,
    issuedOn: issuedOn,
    expiresOn: expiresOn,
  );
}
